package provider

import "testing"

func TestOllamaSetModel(t *testing.T) {
	o := &Ollama{host: "http://localhost:11434", model: "llama3"}

	if o.Model() != "llama3" {
		t.Fatalf("initial model = %q, want %q", o.Model(), "llama3")
	}

	o.SetModel("qwen3:8b")
	if o.Model() != "qwen3:8b" {
		t.Errorf("after SetModel = %q, want %q", o.Model(), "qwen3:8b")
	}
}

func TestOllamaImplementsModelSwitcher(t *testing.T) {
	o := &Ollama{model: "test"}
	var p Provider = o

	sw, ok := p.(ModelSwitcher)
	if !ok {
		t.Fatal("Ollama should implement ModelSwitcher")
	}

	sw.SetModel("new")
	if o.Model() != "new" {
		t.Errorf("got %q, want %q", o.Model(), "new")
	}
}

func TestOllamaName(t *testing.T) {
	o := &Ollama{}
	if o.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", o.Name(), "ollama")
	}
}
