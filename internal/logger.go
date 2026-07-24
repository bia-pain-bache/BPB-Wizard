package internal

import (
	"fmt"
	"io"
	"os"
)

type Logger struct {
	color  bool
	writer io.Writer
}

func NewLogger() *Logger {
	return newLogger(os.Stdout)
}

func newLogger(writer io.Writer) *Logger {
	return &Logger{color: true, writer: writer}
}

func (l *Logger) Info(msg string) {
	fmt.Fprintf(l.writer, "%s %s\n", FmtStr("•", ColorBlue, true), msg)
}

func (l *Logger) Success(msg string) {
	fmt.Fprintf(l.writer, "%s %s\n", FmtStr("✓", ColorGreen, true), msg)
}

func (l *Logger) Error(msg string) {
	fmt.Fprintf(l.writer, "%s %s\n", FmtStr("✗", ColorRed, true), msg)
}

func (l *Logger) Fatal(err error) {
	msg := fmt.Sprintf("Failed to install BPB Panel: %s", err.Error())
	l.Error(msg)
	os.Exit(1)
}
