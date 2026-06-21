package database

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	conn *sql.DB
}

type User struct {
	ID        int
	Username  string
	Password  string
	VirtualIP string
	// 請確保這裡有 GroupID，並且是 NullInt64 以處理 NULL 情況
	GroupID sql.NullInt64
}

type Group struct {
	ID       int
	Name     string
	Password string
}

func NewDatabase(path string) (*Database, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// 初始化 Table
	// 注意：這裡加上了 group_id 欄位
	_, err = conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT,
			virtual_ip TEXT UNIQUE,
			group_id INTEGER
		);
		CREATE TABLE IF NOT EXISTS groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			password TEXT
		);
	`)
	if err != nil {
		return nil, err
	}

	return &Database{conn: conn}, nil
}

// [重點修正] GetUser 必須 SELECT group_id
func (db *Database) GetUser(username string) (*User, error) {
	// 修正前的 SQL 可能長這樣: "SELECT id, username, password, virtual_ip FROM users ..."
	// 修正後 (加上 group_id):
	row := db.conn.QueryRow("SELECT id, username, password, virtual_ip, group_id FROM users WHERE username = ?", username)

	var u User
	// Scan 也要多加一個參數
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByIP 同理
func (db *Database) GetUserByIP(ip string) (*User, error) {
	row := db.conn.QueryRow("SELECT id, username, password, virtual_ip, group_id FROM users WHERE virtual_ip = ?", ip)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.VirtualIP, &u.GroupID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser
func (db *Database) CreateUser(username, password, ip string) error {
	_, err := db.conn.Exec("INSERT INTO users (username, password, virtual_ip) VALUES (?, ?, ?)", username, password, ip)
	return err
}

// UpdateUserGroup
func (db *Database) UpdateUserGroup(userID int, groupID *int) error {
	if groupID == nil {
		_, err := db.conn.Exec("UPDATE users SET group_id = NULL WHERE id = ?", userID)
		return err
	}
	_, err := db.conn.Exec("UPDATE users SET group_id = ? WHERE id = ?", *groupID, userID)
	return err
}

// GetGroup
func (db *Database) GetGroup(name string) (*Group, error) {
	row := db.conn.QueryRow("SELECT id, name, password FROM groups WHERE name = ?", name)
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Password)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// CreateGroup
func (db *Database) CreateGroup(name, password string) error {
	_, err := db.conn.Exec("INSERT INTO groups (name, password) VALUES (?, ?)", name, password)
	return err
}
