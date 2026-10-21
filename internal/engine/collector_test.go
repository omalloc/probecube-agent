package engine

import (
	"errors"
	"testing"
	"time"

	v1 "github.com/omalloc/probecube-api/message/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestTaskFromProto(t *testing.T) {
	task := taskFromProto(&v1.TaskMessage{
		TaskId: 7, RunId: 11, TaskSn: "task-7", Protocol: v1.Protocol_PROTOCOL_HTTPS,
		Target: "https://example.com", Timeout: durationpb.New(3 * time.Second),
		Metadata: map[string]string{"header": "value"},
	})

	if task.ID != 7 || task.RunID != 11 || task.TaskSN != "task-7" || task.Proto != HTTPS ||
		task.Target != "https://example.com" || task.Timeout != 3*time.Second ||
		task.Metadata["header"] != "value" {
		t.Fatalf("taskFromProto() = %+v", task)
	}
}

func TestTaskResultProtoPreservesRunID(t *testing.T) {
	payload, ok := toTaskResultProto(&Result{
		TaskID: 7, RunID: 11, TaskSN: "task-7", Proto: HTTPS,
		Success: false, Duration: 25 * time.Millisecond,
		Metrics: map[string]interface{}{"status_code": 503}, Err: errors.New("unavailable"),
	}, "SN-001").(*v1.TaskResult)
	if !ok {
		t.Fatalf("toTaskResultProto() type = %T", payload)
	}
	if payload.GetTaskId() != 7 || payload.GetRunId() != 11 || payload.GetTaskSn() != "task-7" {
		t.Fatalf("toTaskResultProto() = %+v", payload)
	}
	if payload.GetProtocol() != v1.Protocol_PROTOCOL_HTTPS || payload.GetMetadata()["status_code"] != "503" ||
		payload.GetErrors()["message"] != "unavailable" {
		t.Fatalf("toTaskResultProto() details = %+v", payload)
	}
}
