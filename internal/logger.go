package internal

import (
	"fmt"
	"os"
)

type Logger struct {
	color bool
}

func NewLogger() *Logger {
	return &Logger{color: true}
}

func (l *Logger) Info(msg string) {
	fmt.Printf("%s %s\n", FmtStr("•", ColorBlue, true), msg)
}

func (l *Logger) Success(msg string) {
	
	fmt.Printf("%s %s\n", FmtStr("✓", ColorGreen, true), msg)
}

func (l *Logger) Error(msg string) {
	fmt.Printf("%s %s\n", FmtStr("✗", ColorRed, true), msg)
}

func (l *Logger) Fatal(err error) {
	msg := fmt.Sprintf("Failed to install BPB Panel: %s", err.Error())
	l.Error(msg)
	os.Exit(1)
}
