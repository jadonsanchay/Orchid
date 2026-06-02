package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"

	"orchid/internal/domain"
)

// TailorTask is a pluggable workflow step that simulates LLM-based resume tailoring
// by generating a customized executive summary optimized for the top-matched job description.
type TailorTask struct{}

// NewTailorTask constructs a new TailorTask.
func NewTailorTask() *TailorTask {
	return &TailorTask{}
}

// Name returns the registration identifier for the tailor task.
func (t *TailorTask) Name() string {
	return "tailor"
}

// Execute modifies the resume text to highlight relevance for the top-ranked matched job.
func (t *TailorTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[TailorTask] Starting resume tailoring for Run %d", in.RunID)

	resumeText, ok := in.Data["resume_text"].(string)
	if !ok || resumeText == "" {
		return domain.TaskOutput{}, errors.New("missing or empty 'resume_text' in input data")
	}

	matchedJobsVal, exists := in.Data["matched_jobs"]
	if !exists {
		return domain.TaskOutput{}, errors.New("missing 'matched_jobs' in input data")
	}

	matchedJobs, ok := matchedJobsVal.([]any)
	if !ok {
		// Try concrete slice type just in case
		if concreteSlice, isConcrete := matchedJobsVal.([]map[string]any); isConcrete {
			matchedJobs = make([]any, len(concreteSlice))
			for i, v := range concreteSlice {
				matchedJobs[i] = v
			}
		} else {
			return domain.TaskOutput{}, errors.New("invalid 'matched_jobs' structure: expected list of job matches")
		}
	}

	// Prepare output with passthrough data pattern
	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}

	if len(matchedJobs) == 0 {
		log.Printf("[TailorTask] No matched jobs found; propagating original resume unchanged")
		outData["tailored_resume"] = resumeText
		outData["target_job_id"] = ""
		return domain.TaskOutput{Data: outData}, nil
	}

	// Get the top matching job (first element)
	topJob, ok := matchedJobs[0].(map[string]any)
	if !ok {
		return domain.TaskOutput{}, errors.New("invalid top job structure")
	}

	jobID := fmt.Sprintf("%v", topJob["id"])
	title := fmt.Sprintf("%v", topJob["title"])
	company := fmt.Sprintf("%v", topJob["company"])

	// Generate tailored profile summary
	tailoredSummary := fmt.Sprintf("[Tailored Summary for %s - %s]: Highly alignment-focused profile optimized specifically for the %s role at %s, emphasizing core competencies that match their technical challenges.", company, title, title, company)
	tailoredResume := fmt.Sprintf("%s\n\n%s", tailoredSummary, resumeText)

	log.Printf("[TailorTask] Successfully tailored resume for Job ID %s (%s at %s)", jobID, title, company)

	outData["tailored_resume"] = tailoredResume
	outData["target_job_id"] = jobID

	return domain.TaskOutput{Data: outData}, nil
}
