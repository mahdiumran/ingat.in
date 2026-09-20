package migrations

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMigrationFiles(t *testing.T) {
	want := []string{
		"00001_init.sql",
		"00002_seed.sql",
		"00003_add_template_null_index.sql",
		"00004_fix_notification_templates.sql",
		"00005_add_master_data.sql",
		"00006_add_customer_master_data.sql",
		"00007_update_task_workflow.sql",
		"00008_add_ticket_fields_and_master_data.sql",
		"00009_add_notification_descriptions.sql",
		"00010_add_daily_tasks.sql",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("migration files = %v, want %v", got, want)
	}

	for _, name := range want {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(content)
		if !strings.Contains(text, "-- +goose Up") {
			t.Errorf("%s missing goose Up marker", name)
		}
		if !strings.Contains(text, "-- +goose Down") {
			t.Errorf("%s missing goose Down marker", name)
		}
	}
}
