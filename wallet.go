package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "3333"
	CONN_TYPE = "tcp"
)

type Wallet struct {
	User_id int `json:"user_id"`
	Balance int `json:"balance"`
}

type Prep struct {
	Id      int
	Tx      *sql.Tx
	User_id int
}

type SafeCounter struct {
	v   []int
	mux sync.Mutex
}

var c SafeCounter

func errorHandler(err error) {
	if err != nil {
		panic(err.Error())
	}
}
func handlePrepare(conn net.Conn, password string) Prep {
	buf := make([]byte, 2048)

	_, err := conn.Read(buf)

	if err != nil {
		fmt.Println("Error reading:", err.Error())
		return Prep{10, nil, 0}
	}

	message := string(buf[:2048])
	temp := strings.Split(message, " ")
	user_id, _ := strconv.Atoi(temp[0])
	price, _ := strconv.Atoi(temp[1])

	fmt.Println(user_id, price)

	if err != nil {
		fmt.Println("Error reading:", err.Error())
		return Prep{3, nil, user_id}
	}
	c.mux.Lock()
	for _, n := range c.v {
		if user_id == n {
			fmt.Println("user_id already in list of prepared transactions")
			c.mux.Unlock()
			return Prep{11, nil, user_id}
		}
	}
	fmt.Println(c.v)
	c.v = append(c.v, user_id)
	c.mux.Unlock()
	db, err := sql.Open("mysql", "dilawar:"+password+"@tcp(127.0.0.1:3306)/wallet_service")
	if err != nil {
		//conn.Write([]byte(strconv.Itoa(4))) // 4 = Error connecting to database
		return Prep{4, nil, user_id}
	}

	defer db.Close()

	results, err := db.Query("SELECT * FROM wallet WHERE user_id=?", user_id)
	if err != nil {
		//conn.Write([]byte(strconv.Itoa(5))) // Query went wrong
		return Prep{5, nil, user_id}
	}

	var wallet Wallet
	for results.Next() {

		err = results.Scan(&wallet.User_id, &wallet.Balance)
		if err != nil {
			//conn.Write([]byte(strconv.Itoa(6))) // 6 = Wrong format on wallet object
			return Prep{6, nil, user_id}
		}
	}
	fmt.Println("Wallet :", wallet)
	if wallet.User_id == 0 { // No user
		return Prep{12, nil, user_id}
	}

	tx, err := db.Begin()
	if err != nil {
		//conn.Write([]byte(strconv.Itoa(7))) // Could not start transaction
		return Prep{7, tx, user_id}
	}

	res, err := tx.Exec("UPDATE wallet SET balance=? WHERE user_id=?", wallet.Balance-price, user_id)
	fmt.Println(res.RowsAffected())

	if wallet.Balance-price >= 0 {
		if err != nil {
			tx.Rollback()
			//conn.Write([]byte(strconv.Itoa(8))) // 8 = Could not lock row
			return Prep{8, tx, user_id}
		}
		return Prep{1, tx, user_id}
	} else {
		tx.Rollback()
		return Prep{9, tx, user_id}
	}
}

func handleCommit(conn net.Conn, tx *sql.Tx, user_id int) {

	buf := make([]byte, 2048)
	_, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
	}
	message := string(buf[:2048])
	temp := strings.Split(message, " ")
	id, _ := strconv.Atoi(temp[0])
	if id == 1 {
		err = tx.Commit()
		if err != nil {
			conn.Write([]byte(strconv.Itoa(10))) // Could not COMMIT
		}
		conn.Write([]byte(strconv.Itoa(2))) // 2 = OK COMMIT
	} else if tx != nil {
		tx.Rollback()
	} else {
		fmt.Println("do nothing, transaction never started")
	}
	c.mux.Lock()
	for i := 0; i < len(c.v); i++ {
		if user_id == c.v[i] {
			c.v[i] = c.v[len(c.v)-1] // Copy last element to index i.
			c.v[len(c.v)-1] = 0      // Erase last element (write zero value).
			c.v = c.v[:len(c.v)-1]
		}
	}
	c.mux.Unlock()
}

func main() {
	c = SafeCounter{v: []int{}}
	data, err := ioutil.ReadFile(".config")
	if err != nil {
		fmt.Println("File reading error", err)
		os.Exit(1)
	}
	password := string(data)

	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}

	defer l.Close()
	fmt.Println("Listening on " + CONN_HOST + ":" + CONN_PORT)
	for {
		conn, err := l.Accept()

		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		go prepareAndCommit(conn, password)
	}
}

func prepareAndCommit(conn net.Conn, password string) {
	prep := handlePrepare(conn, password) // skriver her til Coordinator
	tx := prep.Tx
	user_id := prep.User_id
	conn.Write([]byte(strconv.Itoa(prep.Id)))
	handleCommit(conn, tx, user_id)
	conn.Close()
}
