/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	fluxhelmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	fluxsourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ncv1beta1 "github.com/waveywaves/nextcloud-operator/api/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"encoding/json"
)

const (
	HelmRepositoryKind          = "HelmRepository"
	NextcloudHelmRepositoryName = "nextcloud"
	NextcloudHelmChartName      = "nextcloud"
	NextcloudHelmChartVersion   = "6.1.0"
)

// NextcloudReconciler reconciles a Nextcloud object
type NextcloudReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type HelmValues struct {
	Nextcloud struct {
		Host     string `yaml:"host" json:"host"`
		Username string `yaml:"username" json:"username"`
		Password string `yaml:"password" json:"password"`
	} `yaml:"nextcloud" json:"nextcloud"`
	Mariadb struct {
		Enabled bool `yaml:"enabled" json:"enabled"`
	} `yaml:"mariadb" json:"mariadb"`
	Service struct {
		Type string `yaml:"type" json:"type"`
	} `yaml:"service" json:"service"`
}

//+kubebuilder:rbac:groups=nc.waveywaves.com,resources=nextclouds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nc.waveywaves.com,resources=nextclouds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nc.waveywaves.com,resources=nextclouds/finalizers,verbs=update

func (r *NextcloudReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	logger := log.FromContext(ctx)

	// get the Nextcloud CR which was created by the user. This reconciliation loop will be triggered by the creation of this CR,
	// deletion and updation as well. Considering we are only handling creation, we are going to ignore the other events by
	// if the related HelmRelease already exists.
	nc := &ncv1beta1.Nextcloud{}
	if err := r.Get(ctx, req.NamespacedName, nc); err != nil {
		// possibly a delete event
		if k8serrors.IsNotFound(err) {
			logger.Info("Nextcloud deleted", "NamespacedName", req.NamespacedName)
		} else {
			logger.Info("Failed to get Nextcloud", "NamespacedName", req.NamespacedName, "Error", err)
		}
		// Handle error
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// create a patch helper to help us merge any changes made to the Nextcloud CR back into the CR
	patch := client.MergeFrom(nc.DeepCopy())

	// the HelmRelease needs a HelmRepository to know where to pull the HelmChart from
	ncHelmRepository := fluxsourcev1beta2.HelmRepository{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      NextcloudHelmRepositoryName,
			Namespace: nc.Namespace,
		},
		Spec: fluxsourcev1beta2.HelmRepositorySpec{
			URL: "https://nextcloud.github.io/helm/",
		},
	}
	// create the repository if it doesn't already exist in the namespace
	if err := r.Create(ctx, &ncHelmRepository); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			logger.Info("HelmRepository already exists", "NamespacedName", req.NamespacedName)
		} else {
			logger.Info("Failed to create HelmRepository", "NamespacedName", req.NamespacedName, "Error", err)
		}
	}

	// set the helm values based on the helm upgrade command
	helmValues := HelmValues{}

	helmValues.Nextcloud.Host = "127.0.0.1"
	helmValues.Nextcloud.Username = "admin"
	helmValues.Nextcloud.Password = "changeme"
	helmValues.Mariadb.Enabled = false
	helmValues.Service.Type = "ClusterIP"

	// Marshalling to JSONRaw to be able to set it in the HelmRelease
	helmValuesJSONRaw, err := json.Marshal(&helmValues)
	if err != nil {
		logger.Info("Failed to marshal HelmValues to JSON", "NamespacedName", req.NamespacedName, "Error", err)
		return ctrl.Result{}, err
	}

	// create the HelmRelease for Nextcloud
	newHelmRelease := &fluxhelmv2beta1.HelmRelease{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      nc.Name,
			Namespace: nc.Namespace,
		},
		Spec: fluxhelmv2beta1.HelmReleaseSpec{
			Upgrade: &fluxhelmv2beta1.Upgrade{
				Force: false,
			},
			Chart: &fluxhelmv2beta1.HelmChartTemplate{
				Spec: fluxhelmv2beta1.HelmChartTemplateSpec{
					Chart:   NextcloudHelmChartName,
					Version: NextcloudHelmChartVersion,
					SourceRef: fluxhelmv2beta1.CrossNamespaceObjectReference{
						Kind:      HelmRepositoryKind,
						Name:      NextcloudHelmRepositoryName,
						Namespace: nc.Namespace,
					},
				},
			},
			// set the values for the HelmRelease
			Values: &v1.JSON{
				Raw: helmValuesJSONRaw,
			},
		},
	}

	// set a controller reference so that when wee delete the nextcloud CR, the HelmRelease will also be deleted
	if err := controllerutil.SetControllerReference(nc, newHelmRelease, r.Scheme); err != nil {
		logger.Info("Failed to set controller reference", "NamespacedName", req.NamespacedName, "Error", err)
	}

	// create the helm release if it doesn't already exist in the namespace
	if err := r.Create(ctx, newHelmRelease); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			logger.Info("HelmRelease already exists", "NamespacedName", req.NamespacedName)
		} else {
			logger.Info("Failed to create HelmRelease", "NamespacedName", req.NamespacedName, "Error", err)
		}
	}

	// update the Nextcloud CR with the HelmRelease name
	nc.Status.HelmReleaseName = newHelmRelease.Name

	// patch the status of the Nextcloud CR
	if err := r.Status().Patch(ctx, nc, patch); err != nil {
		logger.Info("Failed to patch Nextcloud CR", "NamespacedName", req.NamespacedName, "Error", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NextcloudReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ncv1beta1.Nextcloud{}).
		Complete(r)
}
