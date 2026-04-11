package main

import "crypto/tls"

var cfg = tls.Config{ InsecureSkipVerify: true }
