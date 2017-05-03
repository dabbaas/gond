package main

import (
    "os/exec"
    "log"
)

func createKubeCtlCmd(uuid string) (*exec.Cmd, error) {
    // TODO: Query dhakkan to get command flags
    log.Printf(uuid)
    kubecmd := exec.Command("kubectl", "exec", "-it", "hello-minikube-938614450-z5tbt", "/bin/bash")
    return kubecmd, nil
}
