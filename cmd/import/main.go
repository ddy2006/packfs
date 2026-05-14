package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: import <directory>")
		return
	}

	insertDataSet := `
	INSERT INTO t_dataset (name,relative_path,label)
	VALUES($1,$2,$3)
	RETURNING id
	`

	dir := os.Args[1]

	fmt.Printf("Importing files from directory:%s\n", dir)

	db, err := sql.Open("sqlite3", "../../data/packfs.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var id int
	err = db.QueryRow(insertDataSet, "test", "test", "test").Scan(&id)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Inserted dataset with ID: %d\n", id)

	// err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
	// 	if err != nil {
	// 		return err
	// 	}
	// 	if !info.IsDir() {
	// 		fmt.Printf("Importing file: %s\n", path)
	// 		err = importFile(db, path)
	// 		if err != nil {
	// 			log.Printf("Failed to import file %s: %v\n", path, err)
	// 		}
	// 	}
	// 	return nil
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	fmt.Println("Import completed.")
}

func importFile(db *sql.DB, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// 这里可以根据实际需求解析每行数据并插入到数据库中
		fmt.Printf("Read line: %s\n", line)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
