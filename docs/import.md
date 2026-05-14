# 将目录导入数据库中
## 1. 读取文件目录下的所有文件路径
可以使用递归的方法来读取目录下的所有文件路径。以下是一个示例代码:

```golang 
package main
import (
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
)
func main() {
    var filePaths []string
    root := "./your_directory" // 替换为你的目录路径

    err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if !d.IsDir() {
            filePaths = append(filePaths, path)
        }
        return nil
    })
    if err != nil {
        fmt.Println("Error walking the path:", err)
        return
    }

    // 输出所有文件路径
    for _, filePath := range filePaths {
        fmt.Println(filePath)
    }
}

```
## 2. 将文件路径写入数据库中
可以使用数据库驱动来连接数据库并执行插入操作。以下是一个示例代码,假设使用的是sqlite数据库:

```golang
package main
import (
    "database/sql"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
)
func main() {
    db, err := sql.Open("sqlite3", "./file_paths.db")
    if err != nil {
        fmt.Println("Error opening database:", err)
        return
    }
    defer db.Close()
    // 创建表
    _, err = db.Exec("CREATE TABLE IF NOT EXISTS file_paths (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT)")
    if err != nil {
        fmt.Println("Error creating table:", err)
        return
    }
    // 插入文件路径
    filePaths := []string{"path/to/file1", "path/to/file2"} // 替换为实际的文件路径列表
    for _, filePath := range filePaths {
        _, err = db.Exec("INSERT INTO file_paths (path) VALUES (?)", filePath)
        if err != nil {
            fmt.Println("Error inserting file path:", err)
            return
        }
    }
    fmt.Println("File paths inserted successfully")
}
```
以上代码示例展示了如何读取目录下的所有文件路径并将其写入数据库中。可以根据实际需求进行调整，例如添加错误处理、优化性能等。