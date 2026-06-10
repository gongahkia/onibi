package auth

import "time"

// nowUnix is a thin wrapper to allow test injection if needed later.
func nowUnix() int64 { return time.Now().Unix() }
