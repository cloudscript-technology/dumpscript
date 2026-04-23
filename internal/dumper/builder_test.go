package dumper

import (
	"reflect"
	"testing"
)

func TestArgBuilder_Add_DropsEmpty(t *testing.T) {
	got := NewArgBuilder().Add("-h", "", "host", "").Build()
	want := []string{"-h", "host"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestArgBuilder_AddRaw_SplitsWhitespace(t *testing.T) {
	got := NewArgBuilder().AddRaw("  --no-owner   --clean \t--if-exists ").Build()
	want := []string{"--no-owner", "--clean", "--if-exists"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestArgBuilder_AddRaw_Empty(t *testing.T) {
	got := NewArgBuilder().AddRaw("").Build()
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestArgBuilder_Chain(t *testing.T) {
	got := NewArgBuilder().
		Add("-h", "host").
		AddRaw("--flag1 --flag2").
		Add("-p", "5432").
		Build()
	want := []string{"-h", "host", "--flag1", "--flag2", "-p", "5432"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestArgBuilder_Empty(t *testing.T) {
	got := NewArgBuilder().Build()
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}
