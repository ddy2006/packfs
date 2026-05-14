package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	//从sqlite.sql文件创建sqlite数据库
	sqlFile, err := os.ReadFile("sqlite.sql")
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite3", "../../data/packfs.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(string(sqlFile))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Database created successfully.")

}
