package logx

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

type entry struct {
	Time   string         `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

var (
	once       sync.Once
	debugLog   bool
	stderrLock sync.Mutex
)

func Debug(msg string, fields map[string]any) {
	once.Do(initConfig)
	if !debugLog {
		return
	}
	write("debug", msg, fields)
}

func Warn(msg string, fields map[string]any) {
	write("warn", msg, fields)
}

func initConfig() {
	debugLog = strings.EqualFold(os.Getenv("AGENTMUX_LOG_LEVEL"), "debug")
}

func write(level, msg string, fields map[string]any) {
	e := entry{
		Time:   time.Now().Format(time.RFC3339),
		Level:  level,
		Msg:    msg,
		Fields: fields,
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	stderrLock.Lock()
	defer stderrLock.Unlock()
	_, _ = os.Stderr.Write(append(b, '\n'))
}
