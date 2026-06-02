package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"

	"orchid/internal/domain"
)

// ApplyTask is a pluggable workflow step that simulates the submission of the tailored
// resume to the targeted employer job application portal.
type ApplyTask struct{}

// NewApplyTask constructs a new ApplyTask.
func NewApplyTask() *ApplyTask {
	return &ApplyTask{}
}

// Name returns the registration identifier for the apply task.
func (t *ApplyTask) Name() string {
	return "apply"
}

// Execute validates requirements and logs the application submission details.
func (t *ApplyTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[ApplyTask] Starting application submission for Run %d", in.RunID)

	tailoredResume, ok := in.Data["tailored_resume"].(string)
	if !ok || tailoredResume == "" {
		return domain.TaskOutput{}, errors.New("missing or empty 'tailored_resume' in input data")
	}

	targetJobID, ok := in.Data["target_job_id"].(string)
	if !ok || targetJobID == "" {
		return domain.TaskOutput{}, errors.New("missing or empty 'target_job_id' in input data")
	}

	// Prepare output with passthrough data pattern
	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}

	confirmation := fmt.Sprintf("APP-%d-MOCK", in.RunID)
	log.Printf("[ApplyTask] Successfully submitted application for Run %d to Job ID %s. Confirmation: %s", in.RunID, targetJobID, confirmation)

	outData["applied_job_id"] = targetJobID
	outData["status"] = "submitted"
	outData["confirmation_code"] = confirmation

	return domain.TaskOutput{Data: outData}, nil
}
