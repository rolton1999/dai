package ffmpeg

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

const (
	diagnosticLimit  = 64 * 1024
	processWaitDelay = 5 * time.Second
)

type execRunner struct{}

func (*execRunner) Run(operationContext context.Context, name string, arguments []string) ([]byte, error) {
	command := exec.CommandContext(operationContext, name, arguments...)
	command.WaitDelay = processWaitDelay
	buffer := &diagnosticBuffer{}
	command.Stdout = buffer
	command.Stderr = buffer
	err := command.Run()

	return buffer.body, err
}

type diagnosticBuffer struct {
	mutex sync.Mutex
	body  []byte
}

func (buffer *diagnosticBuffer) Write(body []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()

	size := len(body)
	if size >= diagnosticLimit {
		buffer.body = append(buffer.body[:0], body[size-diagnosticLimit:]...)
	} else {
		buffer.body = append(buffer.body, body...)
		if len(buffer.body) > diagnosticLimit {
			buffer.body = buffer.body[len(buffer.body)-diagnosticLimit:]
		}
	}

	return size, nil
}
