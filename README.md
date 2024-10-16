# nextcloud-operator

### Dependencies 

Install the following dependencies for the operator to run.

To run the command below you will have to install the [FluxCD CLI tool](https://fluxcd.io/flux/installation/).

```sh
flux install --namespace=flux-system --components="source-controller,helm-controller"
```

### Development

Right now, you can't deploy the operator so run the following commands to run the operator locally and test it.

```sh
make install &&  make run
```

Create an instance of Nextcloud with SQLite with the following command.

```sh
kubectl create -f config/samples/nc_v1beta1_nextcloud.yaml
```

