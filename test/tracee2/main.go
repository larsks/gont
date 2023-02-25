// SPDX-FileCopyrightText: 2023 Steffen Vogel <post@steffenvogel.de>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/stv0g/gont/pkg/trace"
	"go.uber.org/zap"
)

var myID string //nolint:unused,gochecknoglobals

func main() {
	if err := trace.Start(0); err != nil {
		panic(err)
	}
	defer trace.Stop() //nolint:errcheck

	myID = os.Args[1]

	cfg := zap.NewDevelopmentConfig()
	cfg.Level.SetLevel(zap.DebugLevel)
	logger, err := cfg.Build(trace.Log())
	if err != nil {
		panic(err)
	}

	logger = logger.With(zap.Strings("argv", os.Args))

	zap.ReplaceGlobals(logger)

	runtime.Breakpoint()

	if err := traced(); err != nil {
		logger.Fatal("Failed to run traced", zap.Error(err))
	}

	if err := log(); err != nil {
		logger.Fatal("Failed to run log", zap.Error(err))
	}

	if err := ping(); err != nil {
		logger.Fatal("Failed to run ping", zap.Error(err))
	}
}

func traced() error {
	for i := 0; i < 5; i++ {
		myTime(i)

		time.Sleep(1 * time.Second)
	}

	return nil
}

func myTime(i int) {
	ts := time.Now()

	fmt.Printf("My time is: %s\n", ts)

	data := map[string]any{
		"i": i,
	}

	if err := trace.PrintfWithData(data, "My time is: %s\n", ts); err != nil {
		fmt.Println(err)
	}
}

func log() error {
	logger := zap.L().Named("log")

	logger.Named("my_first_logger").Debug("Debug")
	logger.Named("my_second_logger").Warn("Warning")
	logger.Named("my_third_logger").Info("Info")
	logger.Named("my_fourth_logger").Error("Error")

	logger.Info("This is a test",
		zap.String("hallo", "welt"),
		zap.Any("any", map[string]any{
			"a": 1,
			"b": false,
			"c": nil,
			"d": map[string]any{
				"1": 1 * time.Hour,
				"2": []int{1, 2, 3, 4},
			},
		}),
		zap.Time("in_one_year", time.Now().Add(24*365*time.Hour)),
	)

	return nil
}

func ping() error {
	logger := zap.L().Named("ping")

	for i := 0; i < 5; i++ {
		start := time.Now()
		cmd := exec.Command("ping", "-c", "1", "127.0.0.1")
		cmd.Stdout = os.Stdout
		err := cmd.Run()

		elapsed := time.Since(start)

		logger.Info("Pinged", zap.Duration("rtt", elapsed), zap.Error(err))

		time.Sleep(100 * time.Millisecond)
	}

	return nil
}