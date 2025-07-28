# Epimetheus

Epimetheus is known as the God of the "afterthought" or "hindsight" in Greek
mythology. Given that monitoring is often tacked on at the end of a project,
the name seemed fitting.

This isn't a monitoring tool. It provides a window into Kubernetes & Talos
Linux for a monitoring tool. For most routes, this will return a 200 status
OK only when those resources meet certain conditions. For example, querying
all nodes returns OK only when all nodes are in the Ready state & all failure
conditions are False.

