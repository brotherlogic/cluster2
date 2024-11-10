package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	/*
	** Bootstraps the cluster **
	 */

	// Can we reach the cluster
	res, err := exec.Command("kubectl", "get", "nodes").CombinedOutput()
	if err == nil {
		count := 0
		for _, line := range strings.Split(string(res), "\n")[1:] {
			elems := strings.Fields(line)
			if elems[0] == "klust1" || elems[0] == "klust2" || elems[0] == "klust3" {
				count++
			}
		}

		// We've seen the three principal nodes
		if count == 3 {
			return
		}
	} else {
		fmt.Printf("kubectl error: %v", err)
	}

	fmt.Printf("Cluster setup error\n")
}
