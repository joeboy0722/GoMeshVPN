package database

import (
	"database/sql"
	"encoding/binary"
	"net"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	conn *sql.DB
}

type User struct {
	ID        int           `json:"id"`
	Username  string        `json:"username"`
	Password  string        `json:"password"`
	VirtualIP string        `json:"virtual_ip"`
	GroupID   sql.NullInt64 `json:"group_id"` // Deprecated but kept for compatibility
}

type Group struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

func NewDatabase(path string) (*Database, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT,
			virtual_ip TEXT UNIQUE,
			group_id INTEGER -- Deprecated
		);
		CREATE TABLE IF NOT EXISTS groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			password TEXT
		);
		CREATE TABLE IF NOT EXISTS user_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			group_id INTEGER,
			UNIQUE(user_id, group_id)
		);
	`)
	if err != nil {
		return nil, err
	}

	return &Database{conn: conn}, nil
}

func (db *Database) GetUser(username string) (*User, error) {
	row := db.conn.QueryRow("SELECT id, username, password, virtual_ip, group_id FROM users WHERE username = ?", username)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *Database) GetUserByIP(ip string) (*User, error) {
	row := db.conn.QueryRow("SELECT id, username, password, virtual_ip, group_id FROM users WHERE virtual_ip = ?", ip)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *Database) CreateUser(username, password, ip string) error {
	_, err := db.conn.Exec("INSERT INTO users (username, password, virtual_ip) VALUES (?, ?, ?)", username, password, ip)
	return err
}

func (db *Database) GetMaxVirtualIP() (string, error) {
	// Since virtual_ip is TEXT, MAX() works alphabetically which is WRONG for IPs (10.0.0.10 < 10.0.0.2).
	// We need to fetch all and find max in Go, or assume sequentially added.
	// For correctness with minimal change: Fetch all IPs and find max int value.
	// Optimization for large scale: This query is O(N).
	// But it only runs ONCE at startup.

	rows, err := db.conn.Query("SELECT virtual_ip FROM users")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var maxIP uint32 = 0
	found := false

	for rows.Next() {
		var ipStr string
		if err := rows.Scan(&ipStr); err != nil {
			continue
		}
		// Convert Function needed here, but we are in `database`, helper is in `core`.
		// We can't import `core` here due to cycle.
		// Duplicate stringToIP logic or move helper to `pkg/utils` or `pkg/protocol`.
		// Local helper:
		ip := net.ParseIP(ipStr).To4()
		if ip == nil {
			continue
		}
		val := binary.BigEndian.Uint32(ip)
		if val > maxIP {
			maxIP = val
			found = true
		}
	}

	if !found {
		return "", nil // None found
	}

	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, maxIP)
	return net.IP(bytes).String(), nil
}

func (db *Database) UpdateUserGroup(userID int, groupID *int) error {
	if groupID == nil {
		_, err := db.conn.Exec("UPDATE users SET group_id = NULL WHERE id = ?", userID)
		return err
	}
	_, err := db.conn.Exec("UPDATE users SET group_id = ? WHERE id = ?", *groupID, userID)
	return err
}

func (db *Database) AddUserToGroup(userID, groupID int) error {
	_, err := db.conn.Exec("INSERT OR IGNORE INTO user_groups (user_id, group_id) VALUES (?, ?)", userID, groupID)
	return err
}

func (db *Database) RemoveUserFromGroup(userID, groupID int) error {
	_, err := db.conn.Exec("DELETE FROM user_groups WHERE user_id = ? AND group_id = ?", userID, groupID)
	return err
}

func (db *Database) GetUserGroups(userID int) ([]Group, error) {
	rows, err := db.conn.Query(`
		SELECT g.id, g.name, g.password 
		FROM groups g 
		JOIN user_groups ug ON g.id = ug.group_id 
		WHERE ug.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Password); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (db *Database) GetGroupMembers(groupID int) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT u.id, u.username, u.password, u.virtual_ip, u.group_id
		FROM users u
		JOIN user_groups ug ON u.id = ug.user_id
		WHERE ug.group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *Database) GetAllUsers() ([]User, error) {
	rows, err := db.conn.Query("SELECT id, username, password, virtual_ip, group_id FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID)
		if err != nil {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *Database) GetGroup(name string) (*Group, error) {
	row := db.conn.QueryRow("SELECT id, name, password FROM groups WHERE name = ?", name)
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Password)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (db *Database) CreateGroup(name, password string) error {
	_, err := db.conn.Exec("INSERT INTO groups (name, password) VALUES (?, ?)", name, password)
	return err
}

func (db *Database) HasCommonGroup(userID1, userID2 int) (bool, error) {
	query := `
		SELECT COUNT(*) FROM user_groups ug1
		JOIN user_groups ug2 ON ug1.group_id = ug2.group_id
		WHERE ug1.user_id = ? AND ug2.user_id = ?
	`
	var count int
	err := db.conn.QueryRow(query, userID1, userID2).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// --- Management API ---

func (db *Database) DeleteUser(id int) error {
	_, err := db.conn.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec("DELETE FROM user_groups WHERE user_id = ?", id)
	return err
}

func (db *Database) GetAllGroups() ([]Group, error) {
	rows, err := db.conn.Query("SELECT id, name, password FROM groups")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Password); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (db *Database) DeleteGroup(id int) error {
	_, err := db.conn.Exec("DELETE FROM groups WHERE id = ?", id)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec("DELETE FROM user_groups WHERE group_id = ?", id)
	return err
}
