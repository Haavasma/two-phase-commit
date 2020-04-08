package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"

	"../micro"
	_ "github.com/go-sql-driver/mysql"
)

const CONN_PORT = "3334"

var list micro.List

type Order struct {
	User_id int `json:"user_id"`
	Amount  int `json:"amount"`
}

func handlePrepare(conn net.Conn, password string) micro.Prep {
	p := make([]byte, 16)
	_, err := conn.Read(p)

	if err != nil {
		fmt.Println("Error reading:", err.Error())
		conn.Write([]byte(strconv.Itoa(3))) // 3 = Error reading
		conn.Close()
		return micro.Prep{3, nil, 0}
	}

	data := binary.BigEndian.Uint32(p[:4])
	user_id := int(data)

	data = binary.BigEndian.Uint32(p[4:])
	amount := int(data)

	fmt.Println(user_id, amount)

	db, err := sql.Open("mysql", "haavasm:"+password+"@tcp(localhost:3306)/order_service")
	if err != nil {
		return micro.Prep{4, nil, user_id}
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil { //7 = could not start transaction
		return micro.Prep{7, tx, user_id}
	}

	res, err := tx.Exec("INSERT INTO `order` (order_id, user_id, amount) VALUES (DEFAULT, ?, ?)", user_id, amount)
	if err != nil {
		tx.Rollback() // 8 = Could not lock row
		return micro.Prep{8, tx, user_id}
	}
	fmt.Println(res.RowsAffected())

	list.Mux.Lock()
	if list.List[user_id] {
		fmt.Println("user_id already in list of prepared transactions")
		list.Mux.Unlock()
		return micro.Prep{11, nil, user_id}
	}
	list.List[user_id] = true
	list.Mux.Unlock()
	return micro.Prep{1, tx, user_id}
}

func main() {
	list = micro.List{List: make(map[int]bool)}
	data, err := ioutil.ReadFile("../.config")
	if err != nil {
		fmt.Println("File reading error", err)
		os.Exit(1)
	}
	password := string(data)

	socket, err := net.Listen(micro.CONN_TYPE, micro.CONN_HOST+":"+CONN_PORT)
	if err != nil {
		fmt.Println("Error listening: ", err.Error())
		os.Exit(1)
	}
	defer socket.Close()
	fmt.Println("Listening on " + micro.CONN_HOST + ":" + CONN_PORT)

	for {
		conn, err := socket.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		go prepareAndCommit(conn, password)
	}
}

func prepareAndCommit(conn net.Conn, password string) {
	//defer return
	prep := handlePrepare(conn, password) // skriver her til Coordinator
	tx := prep.Tx
	user_id := prep.User_id
	b := make([]byte, 2)
	fmt.Println(prep.Id)
	binary.LittleEndian.PutUint16(b, uint16(prep.Id))
	conn.Write(b)
	micro.HandleCommit(conn, tx, user_id, list)
}