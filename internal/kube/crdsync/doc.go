// Package crdsync mirrors the Grus custom resources (Environment, Group, Pool of API group
// grus.onelitefeather.net) of one namespace into the controller. A custom resource is applied with source
// kubernetes, which makes the stored object read-only through the REST API; deleting the resource removes
// it again. The outcome of every apply, and for pools the replica counts, are written back to the status
// subresource.
//
// The syncer is level-based: every event only triggers a reconcile of that one object from the informer
// cache, so a missed or reordered event cannot leave the store behind. Only the controller imports this
// package; the agent never touches Kubernetes.
package crdsync
