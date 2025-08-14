# Goals

The goal of the cluster is to support kubernetes deployment easily with a method
to grow outwards. We only expand nodes when we're close to running out of compute.

## Bootstrap

So we start out with the turing pi cluster, with four nodes. The key thing is to 
track and monitor the overall compute and then plan and expand as we go. So we bootstrap
from this setup, and then we can pull the base functionality as we go.

## Key starting services

1. Ceph
1. Grafana
1. Gramophile

And associated services.

## Key Dashboards

Overall granfana dash - choose from available. 