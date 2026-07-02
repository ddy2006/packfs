package main

import (
	"fmt"
	"os"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/scalebox/pkg/semaphore"
)

func fromNull(body string, headers map[string]string) error {
	// 按defConfig中的配置信息，创建信号量arcset-ok、shard-ok
	numShards := 0
	semaLines := []string{}
	taskLines := []string{}
	for t := defConfig.Dataset.StartTS; t < defConfig.Dataset.EndTS; t += int64(defConfig.Shard.GroupSize) {
		if t > defConfig.Dataset.EndTS+1 {
			t = defConfig.Dataset.EndTS + 1
		}
		for ch := defConfig.Dataset.ChStart; ch <= defConfig.Dataset.ChEnd; ch++ {
			semaName := fmt.Sprintf("shark-ok:%s/t%d_%d/ch%d",
				defConfig.Dataset.Name, defConfig.Dataset.StartTS, t, ch)
			semaLine := fmt.Sprintf(`"%s":%d`, semaName, t-defConfig.Dataset.StartTS)
			semaLines = append(semaLines, semaLine)
			numShards++
		}
		if os.Getenv("ORIGIN_PACKED") == "yes" {
			// 原始数据已打包
			for tt := defConfig.Dataset.StartTS; tt <= t; tt++ {
				taskLine := fmt.Sprintf("%s/%s_%d",
					defConfig.Dataset.Name, defConfig.Dataset.Name, tt)
				taskLines = append(taskLines, taskLine)
			}
		}
	}
	semaName := fmt.Sprintf("arcset-ok:%s", defConfig.Dataset.Name)
	semaLine := fmt.Sprintf(`"%s":%d`, semaName, numShards)
	semaLines = append(semaLines, semaLine)

	// 增加shard-ok
	err := semaphore.CreateSemaphores(semaLines, appID, 100)
	if err != nil {
		return errors.WrapE(err, "semaphore.CreateSemaphores()",
			"app-id", appID, "sema-lines", semaLines)
	}

	// 依据dataset定义及相关环境变量配置，生成pull-untar的任务列表，放到sink-tasks.txt文件
	if os.Getenv("ORIGIN_PACKED") != "" {
		// 原始数据未打包
		for t := defConfig.Dataset.StartTS; t < defConfig.Dataset.EndTS; t++ {
			for ch := defConfig.Dataset.ChStart; ch <= defConfig.Dataset.ChEnd; ch++ {
				taskLine := fmt.Sprintf("%s/%s_%d/ch%d.dat",
					defConfig.Dataset.Name, defConfig.Dataset.Name, t, ch)
				taskLines = append(taskLines, taskLine)
			}
		}
	}

	return nil
}
