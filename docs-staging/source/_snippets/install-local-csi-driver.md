Install the Local CSI Driver to provision PersistentVolumes from local NVMe disks:

:::{code-block} shell
:substitutions:
kubectl -n=local-csi-driver apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/common/local-volume-provisioner/local-csi-driver/{00_clusterrole_def,00_clusterrole_def_openshift,00_clusterrole,00_namespace,00_scylladb-local-xfs.storageclass,10_csidriver,10_serviceaccount,20_clusterrolebinding,50_daemonset}.yaml
:::

Wait for the driver to roll out:

:::{code-block} shell
kubectl -n=local-csi-driver rollout status --timeout=10m daemonset.apps/local-csi-driver
:::
