package advanced

import "os/exec"

var jobs = map[string]string{}

func SaveJob(id string, cmd string) {
    jobs[id] = cmd
}

func RunJob(id string) error {
    cmd := jobs[id]
    return exec.Command("sh", "-c", cmd).Run() // sink
}
