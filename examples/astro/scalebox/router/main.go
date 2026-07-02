package main

import (
	"encoding/json"
	"os"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/logger"
	"github.com/kaichao/scalebox/pkg/common"
	"github.com/sirupsen/logrus"
	// _ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	// logrus.Infoln("00, Entering main-router")
	if len(os.Args) < 3 {
		logrus.Errorf("usage: %s <task-body> <headers>\nparameters expect=2,actual=%d\n",
			os.Args[0], len(os.Args)-1)
		os.Exit(1)
	}

	// logrus.Infof("01, after number of arguments verification, message-body:%s,message-header:%s.\n",
	// 	os.Args[1], os.Args[2])
	headers := make(map[string]string)
	if err := json.Unmarshal([]byte(os.Args[2]), &headers); err != nil {
		logrus.Fatalf("err:%v\n", err)
		os.Exit(2)
	}

	// logrus.Infoln("02, after JSON format verification of headers")
	doMainRoute := fromFuncs[headers["from_module"]]
	if doMainRoute == nil {
		logrus.Warnf("from_module not set/not existed in main-router, from_module=%s ,task-body=%s\n",
			headers["from_module"], os.Args[1])
		os.Exit(4)
	}

	common.AddTimeStamp("before-mr")
	// logrus.Infoln("03, main-router not null")
	err := doMainRoute(os.Args[1], headers)
	if err == nil {
		os.Exit(0)
	}

	logger.LogError(err, logEntry)
	if te, ok := err.(*errors.TracedError); ok {
		os.Exit(te.Code)
	}
	// other error
	os.Exit(1)
}
