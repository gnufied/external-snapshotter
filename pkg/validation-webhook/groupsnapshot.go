/*
Copyright 2023 The Kubernetes Authors.

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

package webhook

import (
	"fmt"

	volumegroupsnapshotv1beta1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	groupsnapshotlisters "github.com/kubernetes-csi/external-snapshotter/client/v8/listers/volumegroupsnapshot/v1beta1"
	"github.com/kubernetes-csi/external-snapshotter/v8/pkg/utils"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

var (
	// GroupSnapshotClassV1Beta1GVR is GroupVersionResource for v1beta1 VolumeGroupSnapshotClasses
	GroupSnapshotClassV1Beta1GVR = metav1.GroupVersionResource{Group: volumegroupsnapshotv1beta1.GroupName, Version: "v1beta1", Resource: "volumegroupsnapshotclasses"}
)

type GroupSnapshotAdmitter interface {
	Admit(v1.AdmissionReview) *v1.AdmissionResponse
}

type groupSnapshotAdmitter struct {
	lister groupsnapshotlisters.VolumeGroupSnapshotClassLister
}

func NewGroupSnapshotAdmitter(lister groupsnapshotlisters.VolumeGroupSnapshotClassLister) GroupSnapshotAdmitter {
	return &groupSnapshotAdmitter{
		lister: lister,
	}
}

// Add a label {"added-label": "yes"} to the object
func (a groupSnapshotAdmitter) Admit(ar v1.AdmissionReview) *v1.AdmissionResponse {
	klog.V(2).Info("admitting volumegroupsnapshotclasses")

	reviewResponse := &v1.AdmissionResponse{
		Allowed: true,
		Result:  &metav1.Status{},
	}

	// Admit requests other than Update and Create
	if !(ar.Request.Operation == v1.Update || ar.Request.Operation == v1.Create) {
		return reviewResponse
	}

	raw := ar.Request.Object.Raw
	oldRaw := ar.Request.OldObject.Raw

	deserializer := codecs.UniversalDeserializer()
	switch ar.Request.Resource {
	case GroupSnapshotClassV1Beta1GVR:
		groupSnapClass := &volumegroupsnapshotv1beta1.VolumeGroupSnapshotClass{}
		if _, _, err := deserializer.Decode(raw, nil, groupSnapClass); err != nil {
			klog.Error(err)
			return toV1AdmissionResponse(err)
		}
		oldGroupSnapClass := &volumegroupsnapshotv1beta1.VolumeGroupSnapshotClass{}
		if _, _, err := deserializer.Decode(oldRaw, nil, oldGroupSnapClass); err != nil {
			klog.Error(err)
			return toV1AdmissionResponse(err)
		}
		return decideGroupSnapshotClassV1Beta1(groupSnapClass, oldGroupSnapClass, a.lister)
	default:
		err := fmt.Errorf("expect resource to be %s, but found %v",
			GroupSnapshotClassV1Beta1GVR, ar.Request.Resource)
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}
}

func decideGroupSnapshotClassV1Beta1(groupSnapClass, oldGroupSnapClass *volumegroupsnapshotv1beta1.VolumeGroupSnapshotClass, lister groupsnapshotlisters.VolumeGroupSnapshotClassLister) *v1.AdmissionResponse {
	reviewResponse := &v1.AdmissionResponse{
		Allowed: true,
		Result:  &metav1.Status{},
	}

	// Only Validate when a new group snapshot class is being set as a default.
	if groupSnapClass.Annotations[utils.IsDefaultGroupSnapshotClassAnnotation] != "true" {
		return reviewResponse
	}

	// If the old group snapshot class has this, then we can assume that it was validated if driver is the same.
	if oldGroupSnapClass.Annotations[utils.IsDefaultGroupSnapshotClassAnnotation] == "true" && oldGroupSnapClass.Driver == groupSnapClass.Driver {
		return reviewResponse
	}

	ret, err := lister.List(labels.Everything())
	if err != nil {
		reviewResponse.Allowed = false
		reviewResponse.Result.Message = err.Error()
		return reviewResponse
	}

	for _, groupSnapshotClass := range ret {
		if groupSnapshotClass.Annotations[utils.IsDefaultGroupSnapshotClassAnnotation] != "true" {
			continue
		}
		if groupSnapshotClass.Driver == groupSnapClass.Driver {
			reviewResponse.Allowed = false
			reviewResponse.Result.Message = fmt.Sprintf("default group snapshot class: %v already exists for driver: %v", groupSnapshotClass.Name, groupSnapClass.Driver)
			return reviewResponse
		}
	}

	return reviewResponse
}
