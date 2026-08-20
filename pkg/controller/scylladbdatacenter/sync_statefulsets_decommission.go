package scylladbdatacenter

import (
	"context"
	"fmt"
	"strconv"

	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	oslices "github.com/scylladb/scylla-operator/pkg/helpers/slices"
	"github.com/scylladb/scylla-operator/pkg/naming"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getRackDecommissionStatus(status *scyllav1alpha1.ScyllaDBDatacenterStatus, rackName string) *scyllav1alpha1.RackDecommissionStatus {
	_, idx, ok := oslices.Find(status.Racks, func(rackStatus scyllav1alpha1.RackStatus) bool {
		return rackStatus.Name == rackName
	})
	if !ok {
		return nil
	}
	return status.Racks[idx].Decommission
}

func setRackDecommissionStatus(status *scyllav1alpha1.ScyllaDBDatacenterStatus, rackName string, decommission *scyllav1alpha1.RackDecommissionStatus) {
	_, idx, ok := oslices.Find(status.Racks, func(rackStatus scyllav1alpha1.RackStatus) bool {
		return rackStatus.Name == rackName
	})
	if !ok {
		return
	}
	status.Racks[idx].Decommission = decommission
}

// getStampedRackServices returns the rack's member services carrying the decommission label, keyed by ordinal.
func getStampedRackServices(rackServices map[string]*corev1.Service) (map[int32]*corev1.Service, error) {
	stamped := map[int32]*corev1.Service{}
	for _, svc := range rackServices {
		if _, ok := svc.Labels[naming.DecommissionedLabel]; !ok {
			continue
		}

		ordinalStrings := serviceOrdinalRegex.FindStringSubmatch(svc.Name)
		if len(ordinalStrings) != 2 {
			return nil, fmt.Errorf("can't parse ordinal from service %q", naming.ObjRef(svc))
		}
		ordinal, err := strconv.Atoi(ordinalStrings[1])
		if err != nil {
			return nil, fmt.Errorf("can't parse ordinal from service %q: %w", naming.ObjRef(svc), err)
		}

		stamped[int32(ordinal)] = svc
	}
	return stamped, nil
}

// ensureRackDecommissionGating commits a rack scale-down to the rack's status before any decommission
// intent is stamped, and clears the record once the operation concludes (the StatefulSet is at the
// committed target and all stamped services are pruned). While the record exists, it overrides the spec
// node count in both directions, so node count changes made during an ongoing scale-down take effect
// only after it concludes; regained capacity bootstraps afresh. The decommission flow itself is
// unchanged. It returns true if it made a change that requires the sync to be retried.
func (sdcc *Controller) ensureRackDecommissionGating(
	ctx context.Context,
	sdc *scyllav1alpha1.ScyllaDBDatacenter,
	status *scyllav1alpha1.ScyllaDBDatacenterStatus,
	sts *appsv1.StatefulSet,
	requiredReplicas int32,
	rackServices map[string]*corev1.Service,
) ([]metav1.Condition, bool, error) {
	var progressingConditions []metav1.Condition

	rackName := sts.Labels[naming.RackNameLabel]

	stampedServices, err := getStampedRackServices(rackServices)
	if err != nil {
		return progressingConditions, false, err
	}

	record := getRackDecommissionStatus(status, rackName)
	if record != nil && record.DesiredNodes != nil {
		// Conclude once the StatefulSet is at the committed target and all stamped services are pruned.
		// Only then does the (possibly changed) spec node count reconcile again.
		if len(stampedServices) == 0 && *sts.Spec.Replicas == *record.DesiredNodes {
			setRackDecommissionStatus(status, rackName, nil)
			err = sdcc.updateStatus(ctx, sdc, status)
			if err != nil {
				return progressingConditions, false, fmt.Errorf("can't clear the decommission status of rack %q: %w", rackName, err)
			}

			progressingConditions = append(progressingConditions, metav1.Condition{
				Type:               statefulSetControllerProgressingCondition,
				Status:             metav1.ConditionTrue,
				Reason:             "RackScaleDownConcluded",
				Message:            fmt.Sprintf("Scale-down of rack %q concluded, resuming normal reconciliation.", rackName),
				ObservedGeneration: sdc.Generation,
			})
			return progressingConditions, true, nil
		}

		return progressingConditions, false, nil
	}

	if requiredReplicas >= *sts.Spec.Replicas {
		return progressingConditions, false, nil
	}

	// Commit the scale-down target to status before any decommission intent is stamped,
	// so the operation survives restarts and later spec changes atomically.
	setRackDecommissionStatus(status, rackName, &scyllav1alpha1.RackDecommissionStatus{
		DesiredNodes: &requiredReplicas,
	})
	err = sdcc.updateStatus(ctx, sdc, status)
	if err != nil {
		return progressingConditions, false, fmt.Errorf("can't commit the decommission status of rack %q: %w", rackName, err)
	}

	progressingConditions = append(progressingConditions, metav1.Condition{
		Type:               statefulSetControllerProgressingCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "CommittingRackScaleDown",
		Message:            fmt.Sprintf("Committed scale-down of rack %q to %d nodes.", rackName, requiredReplicas),
		ObservedGeneration: sdc.Generation,
	})
	return progressingConditions, true, nil
}
