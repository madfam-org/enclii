package main

import (
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
)

// envInt reads an integer from the environment, falling back to def when the
// variable is unset or not a valid positive integer. Used for the DB pool
// budget (DB_MAX_OPEN_CONNS / DB_MAX_IDLE_CONNS) so operators can raise caps
// per-env without a code change. Invalid values warn and fall back rather
// than silently becoming zero — SetMaxOpenConns(0) means UNLIMITED, which is
// the one value the budget exists to forbid.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		logrus.WithField(key, v).Warnf("invalid integer in %s; using default %d", key, def)
	}
	return def
}
