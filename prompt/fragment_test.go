package prompt

import (
	"testing"
)

func TestCategoryOrdering(t *testing.T) {
	if CategorySystemBase >= CategoryToolDirective {
		t.Error("SystemBase should come before ToolDirective")
	}
	if CategoryToolDirective >= CategoryProviderHints {
		t.Error("ToolDirective should come before ProviderHints")
	}
	if CategoryProviderHints >= CategoryRoleContext {
		t.Error("ProviderHints should come before RoleContext")
	}
	if CategoryRoleContext >= CategoryDomainContext {
		t.Error("RoleContext should come before DomainContext")
	}
	if CategoryDomainContext >= CategoryToolGuidance {
		t.Error("DomainContext should come before ToolGuidance")
	}
	if CategoryToolGuidance >= CategoryOutputFormat {
		t.Error("ToolGuidance should come before OutputFormat")
	}
	if CategoryOutputFormat >= CategoryGapDetection {
		t.Error("OutputFormat should come before GapDetection")
	}
}

func TestDefaultProviderStyles(t *testing.T) {
	styles := DefaultProviderStyles()

	anthropic := styles[ProviderAnthropic]
	if !anthropic.PreferXML {
		t.Error("Anthropic should prefer XML")
	}
	if anthropic.PreferMarkdown {
		t.Error("Anthropic should not prefer Markdown")
	}

	openai := styles[ProviderOpenAI]
	if openai.PreferXML {
		t.Error("OpenAI should not prefer XML")
	}
	if !openai.PreferMarkdown {
		t.Error("OpenAI should prefer Markdown")
	}

	ollama := styles[ProviderOllama]
	if !ollama.PreferMarkdown {
		t.Error("Ollama should prefer Markdown")
	}
}
