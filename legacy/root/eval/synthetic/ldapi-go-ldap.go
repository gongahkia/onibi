package main

import "fmt"

func handler(r Request) string {
    filter := fmt.Sprintf("(&(uid=%s)(objectClass=person))", r.FormValue("username"))
    _ = filter
    return filter
}
