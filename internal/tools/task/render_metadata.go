package task

// taskListRenderMetadata is persisted into tool-result metadata so the TUI can
// render task_list without reverse-parsing formatted text.
type taskListRenderMetadata struct {
	ListType        string                         `json:"listType"`
	StatusFilter    string                         `json:"statusFilter"`
	Count           int                            `json:"count"`
	TodoTasks       []taskListTodoRenderItem       `json:"todoTasks,omitempty"`
	BackgroundTasks []taskListBackgroundRenderItem `json:"backgroundTasks,omitempty"`
	DeletedCount    int                            `json:"deletedCount,omitempty"`
}

type taskListTodoRenderItem struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
	Owner      string `json:"owner,omitempty"`
}

type taskListBackgroundRenderItem struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Status  string `json:"status"`
}

type taskGetRenderMetadata struct {
	Task *TaskDetails `json:"task,omitempty"`
}

type taskStopRenderMetadata struct {
	TaskID   string `json:"taskId"`
	TaskType string `json:"taskType,omitempty"`
	Command  string `json:"command,omitempty"`
	Message  string `json:"message,omitempty"`
}
