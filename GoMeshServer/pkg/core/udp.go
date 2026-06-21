package core

import (
	"GoMeshServer/pkg/crypto"
	"GoMeshServer/pkg/protocol"
	"GoMeshServer/pkg/qos"
	"encoding/binary"
	"log"
	"net"
)

// UDP Worker Pool Configuration
const (
	UDPWorkerCount = 16    // Number of concurrent packet processors
	UDPJobQueue    = 10000 // Buffer size for incoming packets
)

type udpJob struct {
	remoteAddr *net.UDPAddr
	data       []byte // Copied from pool
}

func (s *Server) StartUDPLoop() {
	log.Println("UDP Loop Started with Worker Pool")

	// Job Channel
	jobs := make(chan udpJob, UDPJobQueue)

	// Start Workers
	for i := 0; i < UDPWorkerCount; i++ {
		go s.udpWorker(i, jobs)
	}

	// Buffer Pool is initialized in vpn_core.go (or we will add it there, here we assume usage)
	// For raw UDP read, we still need a buffer.
	// To avoid race conditions, we'll allocate a read buffer per loop or use a pool carefully.
	// Since ReadFromUDP blocks, we reuse one buffer per loop iteration and copy data for the worker.

	readBuf := make([]byte, 65535)

	for {
		n, remoteAddr, err := s.udpConn.ReadFromUDP(readBuf)
		if err != nil {
			select {
			case <-s.stopChan:
				close(jobs)
				return
			default:
				log.Printf("UDP Read error: %v", err)
				continue
			}
		}

		// Copy data to a safe buffer for the worker
		// Ideally we use a pool here too to avoid allocation
		poolBuf := bufferPool.Get().([]byte)
		// Ensure capacity
		if cap(poolBuf) < n {
			poolBuf = make([]byte, n)
		} else {
			poolBuf = poolBuf[:n]
		}
		copy(poolBuf, readBuf[:n])

		// Dispatch (Non-blocking drop if critical load)
		select {
		case jobs <- udpJob{remoteAddr: remoteAddr, data: poolBuf}:
		default:
			// Job queue full, drop packet to protect server
			// atomic.AddUint64(&s.droppedPackets, 1)
			bufferPool.Put(poolBuf[:0]) // Return unused buffer
		}
	}
}

func (s *Server) udpWorker(id int, jobs <-chan udpJob) {
	for job := range jobs {
		s.handleUDPPacket(s.udpConn, job.remoteAddr, job.data)

		// Return buffer to pool after handling
		bufferPool.Put(job.data[:0])
	}
}

func (s *Server) handleUDPPacket(conn *net.UDPConn, remoteAddr *net.UDPAddr, data []byte) {
	if len(data) < 9 { // Minimal Header size needed: Type(1) + Source(4) + Target(4)
		return
	}

	// Parse Header
	pktType := data[0]
	sourceIP := binary.BigEndian.Uint32(data[1:5])
	targetIP := binary.BigEndian.Uint32(data[5:9])
	encryptedPayload := data[9:]

	// 1. Identify Source Session
	session := s.sessions.GetSession(sourceIP)
	if session == nil {
		// Unknown source ip, possibly spoofed or session expired
		return
	}

	// 2. Decrypt Payload to verify sender (Authentication tag verification)
	decrypted, err := crypto.Decrypt(encryptedPayload, session.SessionKey)
	if err != nil {
		return
	}

	// 3. Update Real Addr (NAT traversal)
	s.sessions.UpdateRealAddr(sourceIP, remoteAddr)

	switch pktType {
	case protocol.UDPTypeKeepAlive:
		// NAT update already done above. 
		// Echo back to client so they know we are alive and don't timeout.
		conn.WriteToUDP(data, remoteAddr)
		return

	case protocol.UDPTypeData:
		s.forwardData(conn, sourceIP, targetIP, decrypted, pktType)

	case protocol.UDPTypeBroadcast:
		s.broadcastData(conn, sourceIP, decrypted)
	}
}

func (s *Server) forwardData(conn *net.UDPConn, sourceIP, targetIP uint32, payload []byte, pktType byte) {
	targetSess := s.sessions.GetSession(targetIP)
	if targetSess == nil {
		// Ignore Broadcast/Multicast noise in logs
		isBroadcast := targetIP == 0xFFFFFFFF || (targetIP&0xFF) == 255 || (targetIP>>28) == 0xE
		if !isBroadcast {
			// log.Printf("ForwardUDP: Target %s not found", IPToString(targetIP))
		}
		return
	}

	// Thread-safe check for RealAddr
	targetAddr := targetSess.GetRealAddr()
	if targetAddr == nil {
		// log.Printf("ForwardUDP: Target %s has no RealAddr", IPToString(targetIP))
		return
	}

	// Access Control: same group?
	sourceSess := s.sessions.GetSession(sourceIP)
	if sourceSess == nil {
		return
	}

	// Optimized In-Memory Check
	hasCommon := false
	for _, gid1 := range sourceSess.GroupIDs {
		for _, gid2 := range targetSess.GroupIDs {
			if gid1 == gid2 {
				hasCommon = true
				break
			}
		}
		if hasCommon {
			break
		}
	}

	if !hasCommon {
		// log.Printf("[DEBUG] ForwardUDP: No common group between %s and %s", IPToString(sourceIP), IPToString(targetIP))
		return
	}

	// QoS: Classify Packet (using plaintext payload)
	priority := s.sessions.Classifier.Classify(payload)

	encryptedForTarget, err := crypto.Encrypt(payload, targetSess.SessionKey)
	if err != nil {
		return
	}

	// Construct Packet
	msg := bufferPool.Get().([]byte)
	requiredSize := 9 + len(encryptedForTarget)
	if cap(msg) < requiredSize {
		msg = make([]byte, requiredSize)
	} else {
		msg = msg[:requiredSize]
	}

	msg[0] = pktType
	binary.BigEndian.PutUint32(msg[1:5], sourceIP)
	binary.BigEndian.PutUint32(msg[5:9], targetIP)
	copy(msg[9:], encryptedForTarget)

	// QoS: Enqueue instead of direct write
	if targetSess.PacketQueue != nil {
		if !targetSess.PacketQueue.Enqueue(qos.Packet{
			Data:     msg,
			Priority: priority,
		}) {
			// Queue full, drop packet
			// atomic.AddUint64(&s.droppedPackets, 1)
			bufferPool.Put(msg[:0])
		}
	} else {
		// No queue, drop
		bufferPool.Put(msg[:0])
	}
}

func (s *Server) broadcastData(conn *net.UDPConn, sourceIP uint32, payload []byte) {
	peers := s.sessions.GetGroupPeers(sourceIP)

	// QoS: Classify Packet once for all peers
	priority := s.sessions.Classifier.Classify(payload)

	for _, peer := range peers {
		// Thread-safe read of RealAddr
		addr := peer.GetRealAddr()
		if addr == nil {
			continue
		}

		encrypted, err := crypto.Encrypt(payload, peer.SessionKey)
		if err != nil {
			continue
		}

		targetIP := uint32(0xFFFFFFFF)

		msg := bufferPool.Get().([]byte)
		requiredSize := 9 + len(encrypted)
		if cap(msg) < requiredSize {
			msg = make([]byte, requiredSize)
		} else {
			msg = msg[:requiredSize]
		}

		msg[0] = protocol.UDPTypeBroadcast
		binary.BigEndian.PutUint32(msg[1:5], sourceIP)
		binary.BigEndian.PutUint32(msg[5:9], targetIP)
		copy(msg[9:], encrypted)

		// QoS: Enqueue
		if peer.PacketQueue != nil {
			if !peer.PacketQueue.Enqueue(qos.Packet{
				Data:     msg,
				Priority: priority,
			}) {
				// Queue full
				bufferPool.Put(msg[:0])
			}
		} else {
			bufferPool.Put(msg[:0])
		}
	}
}
