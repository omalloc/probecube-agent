package engine

import (
	"testing"
	"time"

	v1 "github.com/omalloc/probecube-console/api/message/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestTaskFromProto(t *testing.T) {
	task := taskFromProto(&v1.TaskMessage{
		TaskId: 7, TaskSn: "task-7", Protocol: v1.Protocol_PROTOCOL_HTTPS,
		Target: "https://example.com", Timeout: durationpb.New(3 * time.Second),
		Metadata: map[string]string{"header": "value"},
	})

	if task.ID != 7 || task.TaskSN != "task-7" || task.Proto != HTTPS ||
		task.Target != "https://example.com" || task.Timeout != 3*time.Second ||
		task.Metadata["header"] != "value" {
		t.Fatalf("taskFromProto() = %+v", task)
	}
}
