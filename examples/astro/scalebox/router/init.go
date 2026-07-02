package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	fromFuncs = map[string]func(string, map[string]string) error{
		"":           fromNull,
		"pull-untar": fromPullUntar,
		"shard-make": fromShardMake,
	}

	logEntry *logrus.Entry

	appID int
)

func init() {
	appID, _ = strconv.Atoi(os.Getenv("APP_ID"))

	level, err := logrus.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetReportCaller(true)
	formatter := &logrus.TextFormatter{
		DisableQuote: true,
	}
	logrus.SetFormatter(formatter)

	// 配置logger
	log := logrus.New()
	log.SetLevel(level)
	if level >= logrus.DebugLevel {
		// debug / trace
		log.SetFormatter(formatter)
	} else {
		log.SetFormatter(&logrus.JSONFormatter{})
	}
	logEntry = logrus.NewEntry(log)

	defFile := os.Getenv("DEF_FILE")
	if defFile == "" {
		defFile = filepath.Join("..", "..", "dataset.def")
	}

	data, err := os.ReadFile(defFile)
	if err != nil {
		logrus.Fatalf("failed to read def file %s: %v", defFile, err)
	}

	defConfig = &DatasetDef{}
	if err := yaml.Unmarshal(data, defConfig); err != nil {
		logrus.Fatalf("failed to parse def file %s: %v", defFile, err)
	}

	logrus.Infof("loaded def config: dataset=%s, ch=%d-%d, ts=%d-%d",
		defConfig.Dataset.Name,
		defConfig.Dataset.ChStart, defConfig.Dataset.ChEnd,
		defConfig.Dataset.StartTS, defConfig.Dataset.EndTS)
}
