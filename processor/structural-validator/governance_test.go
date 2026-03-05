package structuralvalidator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAntiMock_CleanTestFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "foo_test.go")
	content := `package foo

func TestAdd(t *testing.T) {
	if add(1, 2) != 3 {
		t.Fatal("expected 3")
	}
}

func TestSub(t *testing.T) {
	if sub(3, 1) != 2 {
		t.Fatal("expected 2")
	}
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckAntiMock(dir, []string{"foo_test.go"})
	if !result.Passed {
		t.Errorf("expected clean test file to pass, got: %s", result.Stdout)
	}
	if result.Required {
		t.Error("anti-mock check should be advisory (Required: false)")
	}
}

func TestCheckAntiMock_MockHeavyTestFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "bar_test.go")
	content := `package bar

type MockDB struct {}
type MockCache struct {}
type MockQueue struct {}

func TestHandler(t *testing.T) {
	// one test, three mocks
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckAntiMock(dir, []string{"bar_test.go"})
	if result.Passed {
		t.Error("expected mock-heavy test file to fail")
	}
	if result.Required {
		t.Error("anti-mock check should be advisory (Required: false)")
	}
}

func TestCheckAntiMock_NoTestFiles(t *testing.T) {
	result := CheckAntiMock("/tmp", []string{"main.go", "handler.go"})
	if !result.Passed {
		t.Error("expected no test files to pass")
	}
}

func TestCheckAntiMock_EqualMocksAndTests(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "svc_test.go")
	content := `package svc

type MockRepo struct {}

func TestCreate(t *testing.T) {}
func TestDelete(t *testing.T) {}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1 mock, 2 tests → should pass (mockCount <= testCount)
	result := CheckAntiMock(dir, []string{"svc_test.go"})
	if !result.Passed {
		t.Errorf("expected equal-or-fewer mocks to pass, got: %s", result.Stdout)
	}
}

func TestCountMocksAndTests(t *testing.T) {
	src := `package foo

type MockDB struct {}
type mockCache struct {}
type NotAMock struct {}

func TestFoo(t *testing.T) {}
func TestBar(t *testing.T) {}
func helper() {}
`
	mocks, tests := countMocksAndTests(src)
	if mocks != 2 {
		t.Errorf("expected 2 mocks, got %d", mocks)
	}
	if tests != 2 {
		t.Errorf("expected 2 tests, got %d", tests)
	}
}
