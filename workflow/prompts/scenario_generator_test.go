package prompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScenarioGeneratorPrompt_ContainsRequirementDetails(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanTitle:        "Auth Plan",
		PlanGoal:         "Add JWT authentication to the API",
		RequirementTitle: "Login Validation",
		RequirementDesc:  "Users must be able to log in with valid credentials and receive a JWT token.",
	}

	prompt := ScenarioGeneratorPrompt(params)

	if !strings.Contains(prompt, params.RequirementTitle) {
		t.Errorf("prompt missing requirement title %q", params.RequirementTitle)
	}
	if !strings.Contains(prompt, params.RequirementDesc) {
		t.Errorf("prompt missing requirement description %q", params.RequirementDesc)
	}
}

func TestScenarioGeneratorPrompt_ContainsPlanContext(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanTitle:        "Rate Limiter",
		PlanGoal:         "Add rate limiting to /login endpoint",
		RequirementTitle: "Brute Force Protection",
		RequirementDesc:  "The system must block repeated failed login attempts.",
	}

	prompt := ScenarioGeneratorPrompt(params)

	if !strings.Contains(prompt, params.PlanTitle) {
		t.Errorf("prompt missing plan title %q", params.PlanTitle)
	}
	if !strings.Contains(prompt, params.PlanGoal) {
		t.Errorf("prompt missing plan goal %q", params.PlanGoal)
	}
}

func TestScenarioGeneratorPrompt_InstructsGivenWhenThenFormat(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanTitle:        "Auth",
		PlanGoal:         "Add auth",
		RequirementTitle: "Login",
		RequirementDesc:  "Users log in.",
	}

	prompt := ScenarioGeneratorPrompt(params)

	for _, keyword := range []string{"Given", "When", "Then"} {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("prompt missing BDD keyword %q", keyword)
		}
	}
}

func TestScenarioGeneratorPrompt_InstructsThenAsArray(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanTitle:        "Auth",
		PlanGoal:         "Add auth",
		RequirementTitle: "Login",
		RequirementDesc:  "Users log in.",
	}

	prompt := ScenarioGeneratorPrompt(params)

	// Prompt should show Then as an array in the JSON example
	if !strings.Contains(prompt, `"then": [`) {
		t.Error("prompt must show 'then' as a JSON array in the output format example")
	}
}

func TestScenarioGeneratorPrompt_InstructsJSONOutput(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanTitle:        "Plan",
		PlanGoal:         "Goal",
		RequirementTitle: "Req",
		RequirementDesc:  "Desc",
	}

	prompt := ScenarioGeneratorPrompt(params)

	if !strings.Contains(prompt, `"scenarios"`) {
		t.Error("prompt must instruct JSON output with 'scenarios' array")
	}
	if !strings.Contains(prompt, "Return ONLY") {
		t.Error("prompt must instruct to return ONLY JSON")
	}
}

func TestScenarioGeneratorResponse_JSONDeserialization(t *testing.T) {
	raw := `{
		"scenarios": [
			{
				"given": "an unauthenticated user with a registered account",
				"when": "they submit valid credentials",
				"then": [
					"the response status is 200",
					"a JWT token is returned",
					"the token expires in 24 hours"
				]
			},
			{
				"given": "an unauthenticated user",
				"when": "they submit an incorrect password",
				"then": [
					"the response status is 401",
					"no token is returned"
				]
			}
		]
	}`

	var resp ScenarioGeneratorResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(resp.Scenarios) != 2 {
		t.Fatalf("len(Scenarios) = %d, want 2", len(resp.Scenarios))
	}

	s0 := resp.Scenarios[0]
	if s0.Given != "an unauthenticated user with a registered account" {
		t.Errorf("Scenarios[0].Given = %q", s0.Given)
	}
	if s0.When != "they submit valid credentials" {
		t.Errorf("Scenarios[0].When = %q", s0.When)
	}
	if len(s0.Then) != 3 {
		t.Errorf("len(Scenarios[0].Then) = %d, want 3", len(s0.Then))
	}
	if s0.Then[0] != "the response status is 200" {
		t.Errorf("Scenarios[0].Then[0] = %q", s0.Then[0])
	}

	s1 := resp.Scenarios[1]
	if len(s1.Then) != 2 {
		t.Errorf("len(Scenarios[1].Then) = %d, want 2", len(s1.Then))
	}
}

func TestScenarioGeneratorPrompt_NoReviewFindings(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanGoal:         "Add auth",
		RequirementTitle: "Login",
		RequirementDesc:  "Users log in.",
	}

	prompt := ScenarioGeneratorPrompt(params)

	if strings.Contains(prompt, "Review Findings") {
		t.Error("prompt should NOT contain review findings section when ReviewFindings is empty")
	}
}

func TestScenarioGeneratorPrompt_WithReviewFindings(t *testing.T) {
	params := ScenarioGeneratorParams{
		PlanGoal:         "Add auth",
		RequirementTitle: "Login",
		RequirementDesc:  "Users log in.",
		ReviewFindings:   "### Violations\n- Not all requirements have scenarios",
	}

	prompt := ScenarioGeneratorPrompt(params)

	if !strings.Contains(prompt, "Previous Review Findings") {
		t.Error("prompt should contain review findings header")
	}
	if !strings.Contains(prompt, "Not all requirements have scenarios") {
		t.Error("prompt should contain the actual findings text")
	}
}

func TestFormatScenariosForTaskGenerator_Empty(t *testing.T) {
	result := FormatScenariosForTaskGenerator(nil)
	if result != "" {
		t.Errorf("FormatScenariosForTaskGenerator(nil) = %q, want empty", result)
	}
}

func TestFormatScenariosForTaskGenerator_ContainsScenarioIDs(t *testing.T) {
	scenarios := []ScenarioInfo{
		{
			ID:               "scenario.my-plan.1.1",
			RequirementTitle: "Login",
			Given:            "a registered user",
			When:             "they submit valid credentials",
			Then:             []string{"response is 200", "token is returned"},
		},
		{
			ID:               "scenario.my-plan.1.2",
			RequirementTitle: "Login",
			Given:            "a registered user",
			When:             "they submit wrong password",
			Then:             []string{"response is 401"},
		},
	}

	result := FormatScenariosForTaskGenerator(scenarios)

	for _, s := range scenarios {
		if !strings.Contains(result, s.ID) {
			t.Errorf("result missing scenario ID %q", s.ID)
		}
		if !strings.Contains(result, s.Given) {
			t.Errorf("result missing Given %q", s.Given)
		}
		if !strings.Contains(result, s.When) {
			t.Errorf("result missing When %q", s.When)
		}
		for _, outcome := range s.Then {
			if !strings.Contains(result, outcome) {
				t.Errorf("result missing Then outcome %q", outcome)
			}
		}
	}
}
