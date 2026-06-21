package core

import (
	"GoMeshServer/pkg/crypto"
	"GoMeshServer/pkg/protocol"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

func (s *Server) HandleTCP(conn net.Conn) {
	defer conn.Close()
	log.Printf("New TCP Connection from %s", conn.RemoteAddr())

	// 1. Handshake (ECDH)
	sessionKey, err := s.handshake(conn)
	if err != nil {
		log.Printf("Handshake failed: %v", err)
		return
	}
	log.Println("Handshake successful, Session Key established.")

	// 2. Control Loop
	var currentIP uint32 = 0
	var currentSess *Session
	timeoutDur := 30 * time.Second // 30 seconds (Server Deadline，Client 心跳 15s，保留 15s 緩衝)

	for {
		// Set or reset read deadline for heartbeat
		conn.SetReadDeadline(time.Now().Add(timeoutDur))

		// Read Length (4 bytes)
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			if err != io.EOF {
				select {
				case <-s.stopChan:
					return
				default:
					log.Printf("Read error: %v", err)
				}
			}
			break
		}
		length := int(header[0])<<24 | int(header[1])<<16 | int(header[2])<<8 | int(header[3])

		// Read Ciphertext
		ciphertext := make([]byte, length)
		if _, err := io.ReadFull(conn, ciphertext); err != nil {
			log.Printf("Read body error: %v", err)
			break
		}

		// Decrypt
		plaintext, err := crypto.Decrypt(ciphertext, sessionKey)
		if err != nil {
			log.Printf("Decrypt error: %v", err)
			continue
		}

		// Parse JSON
		var pkt protocol.ControlPacket
		if err := json.Unmarshal(plaintext, &pkt); err != nil {
			log.Printf("JSON error: %v", err)
			continue
		}

		// Handle
		resp, ip, sessPtr, err := s.handlePacket(pkt, sessionKey, currentIP, conn)
		if err != nil {
			log.Printf("Handle packet error: %v", err)
			s.sendError(conn, sessionKey, err.Error())
		} else {
			if ip != 0 {
				currentIP = ip
			}
			if sessPtr != nil {
				currentSess = sessPtr
			}

			// Successfully handled packet, update activity
			if currentSess != nil {
				currentSess.UpdateActivity()
			}

			if resp != nil {
				if currentSess != nil {
					currentSess.WriteMu.Lock()
					s.sendPacket(conn, sessionKey, resp)
					currentSess.WriteMu.Unlock()
				} else {
					s.sendPacket(conn, sessionKey, resp)
				}
			}
		}
	}

	if currentIP != 0 && currentSess != nil {
		if currentSess.User != nil {
			groups, err := s.db.GetUserGroups(currentSess.User.ID)
			if err != nil {
				log.Printf("Disconnect: failed to get groups: %v", err)
			}
			s.sessions.RemoveExactSession(currentIP, currentSess)
			log.Printf("Session %s disconnected", IPToString(currentIP))

			for _, g := range groups {
				go s.BroadcastPeers(g.ID, g.Name)
			}
		} else {
			s.sessions.RemoveExactSession(currentIP, currentSess)
		}
	}
}

// handshake performs ECDH exchange
func (s *Server) handshake(conn net.Conn) ([]byte, error) {
	// 設定握手超時，防止連線建立後不發送資料而卡住
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{}) // 握手完成後清除 deadline

	dec := json.NewDecoder(conn)
	var pkt protocol.ControlPacket
	if err := dec.Decode(&pkt); err != nil {
		return nil, fmt.Errorf("read handshake: %v", err)
	}

	if pkt.Type != protocol.TypeHandshake {
		return nil, fmt.Errorf("expected handshake, got %s", pkt.Type)
	}

	var payload protocol.HandshakePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		return nil, err
	}

	// Generate Server Keys
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	// Compute Shared Secret
	shared, err := crypto.ComputeSharedSecret(priv, payload.PublicKey)
	if err != nil {
		return nil, err
	}
	sessionKey := crypto.DeriveSessionKey(shared)

	// Send Server Public Key
	respPayload := protocol.HandshakePayload{PublicKey: pub.Bytes()}
	respBytes, _ := json.Marshal(respPayload)
	respPkt := protocol.ControlPacket{
		Type:    protocol.TypeHandshake,
		Payload: respBytes,
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(respPkt); err != nil {
		return nil, err
	}

	return sessionKey, nil
}

func (s *Server) handlePacket(pkt protocol.ControlPacket, key []byte, currentIP uint32, conn net.Conn) (*protocol.ControlPacket, uint32, *Session, error) {
	switch pkt.Type {
	case protocol.TypeLogin:
		var p protocol.LoginPayload
		json.Unmarshal(pkt.Payload, &p)
		user, err := s.db.GetUser(p.Username)
		if err != nil {
			// Check AutoRegister
			if !s.Config.AutoRegister {
				return nil, 0, nil, fmt.Errorf("user not found and auto-register disabled")
			}

			// Auto-register
			log.Printf("User %s not found, creating...", p.Username)
			newIP, err := s.sessions.AssignIP()
			if err != nil {
				return nil, 0, nil, fmt.Errorf("ip allocation failed: %v", err)
			}
			if err := s.db.CreateUser(p.Username, p.Password, newIP); err != nil {
				return nil, 0, nil, fmt.Errorf("create user failed: %v", err)
			}
			user, err = s.db.GetUser(p.Username)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("fetch created user failed: %v", err)
			}
		} else {
			if user.Password != p.Password {
				return nil, 0, nil, fmt.Errorf("wrong password")
			}
		}

		ipStr := user.VirtualIP
		ip := StringToIP(ipStr)

		// Prepare Session
		groupIDs := make([]int, 0)
		groups, err := s.db.GetUserGroups(user.ID)
		if err == nil {
			for _, g := range groups {
				groupIDs = append(groupIDs, g.ID)
			}
		}

		sess := &Session{
			VirtualIP:  ip,
			SessionKey: key,
			User:       user,
			GroupIDs:   groupIDs,
			Conn:       conn,
		}
		s.sessions.AddSession(ip, sess)
		sess.StartSender(s.udpConn) // Start QoS Sender Loop

		var myGroupName string
		var initialPeers []protocol.Peer

		groups, err = s.db.GetUserGroups(user.ID)
		if err == nil && len(groups) > 0 {
			myGroupName = groups[0].Name
			members, _ := s.db.GetGroupMembers(groups[0].ID)
			for _, m := range members {
				if m.VirtualIP != ipStr {
					memberIP := StringToIP(m.VirtualIP)
					memberSess := s.sessions.GetSession(memberIP)
					initialPeers = append(initialPeers, protocol.Peer{
						Username:  m.Username,
						VirtualIP: m.VirtualIP,
						IsOnline:  memberSess != nil,
					})
				}
			}
		}

		resPayload := protocol.StatusPayload{
			Message:   "Login success",
			VirtualIP: ipStr,
			UdpPort:   s.Config.UdpPort, // Dynamic Port from Config
			GroupName: myGroupName,
			Peers:     initialPeers,
		}

		log.Printf("[DEBUG] Logging in User: %s (IP: %s, Int: %d). Sending Group: %s, Peers: %d",
			user.Username, ipStr, ip, myGroupName, len(initialPeers))

		b, _ := json.Marshal(resPayload)
		resp := &protocol.ControlPacket{Type: protocol.TypeStatus, Payload: b}
		if err := s.sendPacket(conn, key, resp); err != nil {
			s.sessions.RemoveExactSession(ip, sess)
			return nil, 0, nil, err
		}

		if err == nil {
			for _, g := range groups {
				go s.BroadcastPeers(g.ID, g.Name)
			}
		}

		return nil, ip, sess, nil

	case protocol.TypeCreateGroup:
		var p protocol.GroupPayload
		json.Unmarshal(pkt.Payload, &p)

		if p.GroupName == "" {
			return nil, 0, nil, fmt.Errorf("group name required")
		}

		_, err := s.db.GetGroup(p.GroupName)
		if err == nil {
			return nil, 0, nil, fmt.Errorf("group already exists")
		}

		if err := s.db.CreateGroup(p.GroupName, p.Password); err != nil {
			return nil, 0, nil, fmt.Errorf("create group error: %v", err)
		}

		g, err := s.db.GetGroup(p.GroupName)
		if err != nil {
			return nil, 0, nil, err
		}

		sess := s.sessions.GetSession(currentIP)
		if sess != nil && sess.User != nil {
			if err := s.db.AddUserToGroup(sess.User.ID, g.ID); err != nil {
				return nil, 0, nil, err
			}
			go s.BroadcastPeers(g.ID, g.Name)
		}

		if sess != nil {
			updatedUser, err := s.db.GetUser(sess.User.Username)
			if err == nil {
				sess.User = updatedUser
			}
			// Fix: Refresh GroupIDs cache
			newGroups, err := s.db.GetUserGroups(sess.User.ID)
			if err == nil {
				var ids []int
				for _, grp := range newGroups {
					ids = append(ids, grp.ID)
				}
				sess.GroupIDs = ids
			}
		}

		log.Printf("Group created: %s by IP %s", p.GroupName, IPToString(currentIP))
		resPayload := protocol.StatusPayload{Message: "Group created and joined"}
		b, _ := json.Marshal(resPayload)
		return &protocol.ControlPacket{Type: protocol.TypeStatus, Payload: b}, currentIP, nil, nil

	case protocol.TypeJoinGroup:
		var p protocol.GroupPayload
		json.Unmarshal(pkt.Payload, &p)

		g, err := s.db.GetGroup(p.GroupName)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("group '%s' not found", p.GroupName)
		}

		if g.Password != p.Password {
			return nil, 0, nil, fmt.Errorf("wrong group password")
		}

		sess := s.sessions.GetSession(currentIP)
		if sess != nil && sess.User != nil {
			if err := s.db.AddUserToGroup(sess.User.ID, g.ID); err != nil {
				return nil, 0, nil, err
			}
			go s.BroadcastPeers(g.ID, g.Name)
		}

		if sess != nil {
			updatedUser, err := s.db.GetUser(sess.User.Username)
			if err == nil {
				sess.User = updatedUser
			}
			// Fix: Refresh GroupIDs cache
			newGroups, err := s.db.GetUserGroups(sess.User.ID)
			if err == nil {
				var ids []int
				for _, grp := range newGroups {
					ids = append(ids, grp.ID)
				}
				sess.GroupIDs = ids
			}
		}

		log.Printf("User joined group: %s (IP: %s)", p.GroupName, IPToString(currentIP))
		resPayload := protocol.StatusPayload{Message: fmt.Sprintf("Joined group %s", p.GroupName)}
		b, _ := json.Marshal(resPayload)
		return &protocol.ControlPacket{Type: protocol.TypeStatus, Payload: b}, currentIP, nil, nil

	case protocol.TypeLeaveGroup:
		var p protocol.GroupPayload
		json.Unmarshal(pkt.Payload, &p)

		if p.GroupName == "" {
			return nil, 0, nil, fmt.Errorf("leave group requires group_name")
		}

		g, err := s.db.GetGroup(p.GroupName)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("group not found")
		}

		sess := s.sessions.GetSession(currentIP)
		if sess != nil && sess.User != nil {
			if err := s.db.RemoveUserFromGroup(sess.User.ID, g.ID); err != nil {
				return nil, 0, nil, err
			}
			go s.BroadcastPeers(g.ID, g.Name)
			updatedUser, err := s.db.GetUser(sess.User.Username)
			if err == nil {
				sess.User = updatedUser
			}
			// Fix: Refresh GroupIDs cache
			newGroups, err := s.db.GetUserGroups(sess.User.ID)
			if err == nil {
				var ids []int
				for _, grp := range newGroups {
					ids = append(ids, grp.ID)
				}
				sess.GroupIDs = ids
			}
		}

		resPayload := protocol.StatusPayload{
			Message:   fmt.Sprintf("Left group %s", p.GroupName),
			GroupName: p.GroupName,
			Peers:     []protocol.Peer{},
		}
		b, _ := json.Marshal(resPayload)
		return &protocol.ControlPacket{Type: protocol.TypeStatus, Payload: b}, currentIP, nil, nil

	case protocol.TypeHeartbeat:
		// Echo heartbeat back to client
		resp := &protocol.ControlPacket{Type: protocol.TypeHeartbeat, Payload: []byte("{}")}
		return resp, currentIP, nil, nil

	default:
		return nil, currentIP, nil, fmt.Errorf("unknown packet type: %v", pkt.Type)
	}
}

func (s *Server) BroadcastPeers(groupID int, groupName string) {
	members, err := s.db.GetGroupMembers(groupID)
	if err != nil {
		log.Printf("BroadcastPeers: failed to get members: %v", err)
		return
	}

	var allPeers []protocol.Peer
	var targets []*Session

	log.Printf("[DEBUG] BroadcastPeers: Group %s (%d), Members Count: %d", groupName, groupID, len(members))

	s.sessions.mu.RLock()
	for _, u := range members {
		ip := StringToIP(u.VirtualIP)
		sess := s.sessions.sessions[ip]

		peer := protocol.Peer{
			Username:  u.Username,
			VirtualIP: u.VirtualIP,
			IsOnline:  sess != nil,
		}
		allPeers = append(allPeers, peer)

		if sess != nil {
			targets = append(targets, sess)
		}
	}
	s.sessions.mu.RUnlock()

	log.Printf("[DEBUG] Broadcasting %d peers (%d online) for group %s to %d sessions",
		len(allPeers), len(targets), groupName, len(targets))

	payload := protocol.StatusPayload{
		Message:   "Peer List Update",
		GroupName: groupName,
		Peers:     allPeers,
	}
	b, _ := json.Marshal(payload)
	pkt := &protocol.ControlPacket{Type: protocol.TypeStatus, Payload: b}

	for _, sess := range targets {
		go func(ss *Session) {
			ss.WriteMu.Lock()
			defer ss.WriteMu.Unlock()

			if err := s.sendPacket(ss.Conn, ss.SessionKey, pkt); err != nil {
				log.Printf("[ERROR] Failed to send peer list to %s: %v", ss.User.Username, err)
			}
		}(sess)
	}
}

func (s *Server) sendPacket(conn net.Conn, key []byte, pkt *protocol.ControlPacket) error {
	data, err := json.Marshal(pkt)
	if err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(data, key)
	if err != nil {
		return err
	}

	length := len(encrypted)
	buf := make([]byte, 4+length)
	buf[0] = byte(length >> 24)
	buf[1] = byte(length >> 16)
	buf[2] = byte(length >> 8)
	buf[3] = byte(length)
	copy(buf[4:], encrypted)

	_, err = conn.Write(buf)
	return err
}

func (s *Server) sendError(conn net.Conn, key []byte, msg string) {
	p := protocol.StatusPayload{Message: msg}
	b, _ := json.Marshal(p)
	pkt := &protocol.ControlPacket{Type: protocol.TypeError, Payload: b}
	s.sendPacket(conn, key, pkt)
}
