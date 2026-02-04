package workflow

import (
	"regexp"
	"strings"
)

// ParsedTask represents a task extracted from tasks.md.
type ParsedTask struct {
	// ID is the task identifier (e.g., "1.1", "2.3")
	ID string

	// Section is the section header the task belongs to
	Section string

	// Description is the task description text
	Description string

	// Completed indicates if the task checkbox is checked
	Completed bool
}

// taskLinePattern matches markdown checkbox items: - [ ] or - [x]
var taskLinePattern = regexp.MustCompile(`^[-*]\s*\[([ xX])\]\s*(.+)$`)

// sectionPattern matches markdown headers: ## Section Name
var sectionPattern = regexp.MustCompile(`^##\s+(.+)$`)

// ParseTasks parses a tasks.md file into structured task data.
// Tasks are expected to be markdown checkboxes under section headers.
func ParseTasks(content string) ([]ParsedTask, error) {
	var tasks []ParsedTask
	var currentSection string
	sectionTaskCount := make(map[string]int)
	sectionNum := 0

	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for section header
		if matches := sectionPattern.FindStringSubmatch(trimmed); matches != nil {
			currentSection = matches[1]
			sectionNum++
			sectionTaskCount[currentSection] = 0
			continue
		}

		// Check for task item
		if matches := taskLinePattern.FindStringSubmatch(trimmed); matches != nil {
			checkbox := matches[1]
			description := strings.TrimSpace(matches[2])

			// Generate task ID based on section number and task number within section
			if currentSection == "" {
				currentSection = "Tasks"
				if sectionNum == 0 {
					sectionNum = 1
				}
			}
			sectionTaskCount[currentSection]++
			taskID := formatTaskID(sectionNum, sectionTaskCount[currentSection])

			task := ParsedTask{
				ID:          taskID,
				Section:     currentSection,
				Description: description,
				Completed:   checkbox == "x" || checkbox == "X",
			}
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// formatTaskID creates a task ID from section and task numbers.
func formatTaskID(sectionNum, taskNum int) string {
	return strings.Join([]string{
		itoa(sectionNum),
		itoa(taskNum),
	}, ".")
}

// itoa converts an integer to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)
	negative := n < 0
	if negative {
		n = -n
	}

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if negative {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}

// GetTaskStats returns summary statistics for parsed tasks.
func GetTaskStats(tasks []ParsedTask) (total, completed int) {
	total = len(tasks)
	for _, t := range tasks {
		if t.Completed {
			completed++
		}
	}
	return total, completed
}
