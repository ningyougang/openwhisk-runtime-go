package openwhisk

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type LogWriter struct {
	sender chan string
	writer io.Writer
	stream string
}

func (b *LogWriter) Write(p []byte) (n int, err error) {
	size, err := b.writer.Write(p)
	for _, str := range strings.Split(string(p), "\n") {
		if len(str) != 0 {
			log := fmt.Sprintf("%s %s: %s", time.Now().Format(time.RFC3339Nano), b.stream, str)
			b.sender <- log
		}
	}
	return size, err
}

func NewLogWriter(stream string, sender chan string, writer io.Writer) (proc *LogWriter) {
	return &LogWriter{
		sender,
		writer,
		stream,
	}
}
