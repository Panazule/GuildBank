package mariadb

import (
	"context"
	"strings"
	"testing"
)

func TestOpenValidatesBeforeDialing(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing host", Config{Database: "GuildBank", Username: "u"}, "host"},
		{"missing database", Config{Host: "db", Username: "u"}, "database"},
		{"missing username", Config{Host: "db", Database: "GuildBank"}, "username"},
		{"bad port", Config{Host: "db", Port: 0, Database: "GuildBank", Username: "u"}, "port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(context.Background(), tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}
