package testing

// ContainerActionArgs exposes the docker verb allowlist to the external tests.
func ContainerActionArgs(action, id string) ([]string, error) { return containerActionArgs(action, id) }
