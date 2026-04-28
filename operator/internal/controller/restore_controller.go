/*
Copyright 2026.

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
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dumpscriptv1alpha1 "github.com/cloudscript-technology/dumpscript/operator/api/v1alpha1"
)

// RestoreReconciler reconciles a Restore object.
type RestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Condition types and reasons emitted on Restore.status.conditions[].
const (
	RestoreConditionReady  = "Ready"
	RestoreReasonRunning   = "RestoreRunning"
	RestoreReasonSucceeded = "RestoreSucceeded"
	RestoreReasonFailed    = "RestoreFailed"
	RestoreReasonJobError  = "RestoreJobError"
)

// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile materialises a Restore into a one-shot batch/v1 Job and reflects
// the Job's terminal state (Succeeded / Failed) back to the Restore status.
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var restore dumpscriptv1alpha1.Restore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Nothing to do once we reach a terminal phase.
	if restore.Status.Phase == dumpscriptv1alpha1.RestorePhaseSucceeded ||
		restore.Status.Phase == dumpscriptv1alpha1.RestorePhaseFailed {
		return ctrl.Result{}, nil
	}

	desired := buildRestoreJob(&restore)
	if err := controllerutil.SetControllerReference(&restore, desired, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set controller ref: %w", err)
	}

	current := &batchv1.Job{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, current)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			r.eventf(&restore, corev1.EventTypeWarning, RestoreReasonJobError, "failed to create restore Job: %v", err)
			return ctrl.Result{}, fmt.Errorf("create restore job: %w", err)
		}
		log.Info("created restore job", "job", desired.Name)
		r.eventf(&restore, corev1.EventTypeNormal, RestoreReasonRunning, "created restore Job %s", desired.Name)
		err := r.patchRestoreStatus(ctx, &restore, func(s *dumpscriptv1alpha1.RestoreStatus) {
			now := metav1.Now()
			s.Phase = dumpscriptv1alpha1.RestorePhasePending
			s.JobName = desired.Name
			s.StartedAt = &now
			setRestoreReadyCondition(s, restore.Generation)
		})
		return ctrl.Result{}, err
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get restore job: %w", err)
	}

	prevPhase := restore.Status.Phase

	// Reflect Job terminal state back to the Restore status.
	err = r.patchRestoreStatus(ctx, &restore, func(s *dumpscriptv1alpha1.RestoreStatus) {
		s.ObservedGeneration = restore.Generation
		switch {
		case current.Status.Succeeded > 0:
			s.Phase = dumpscriptv1alpha1.RestorePhaseSucceeded
			s.CompletedAt = current.Status.CompletionTime
			s.DurationSeconds = int64(jobDuration(current))
			s.Message = fmt.Sprintf("restore from %s completed successfully", restore.Spec.SourceKey)
		case current.Status.Failed > 0:
			s.Phase = dumpscriptv1alpha1.RestorePhaseFailed
			s.CompletedAt = lastJobConditionTime(current, batchv1.JobFailed)
			s.DurationSeconds = int64(jobDuration(current))
			s.Message = fmt.Sprintf("job %s failed after %d attempt(s) — see pod logs", current.Name, current.Status.Failed)
		case current.Status.Active > 0:
			s.Phase = dumpscriptv1alpha1.RestorePhaseRunning
		}
		setRestoreReadyCondition(s, restore.Generation)
	})
	if err == nil && restore.Status.Phase != prevPhase {
		engine := restore.Spec.Database.Type
		switch restore.Status.Phase {
		case dumpscriptv1alpha1.RestorePhaseSucceeded:
			log.Info("restore succeeded", "job", current.Name)
			r.eventf(&restore, corev1.EventTypeNormal, RestoreReasonSucceeded,
				"restore Job %s completed successfully", current.Name)
			RestoreTotal.WithLabelValues(restore.Namespace, restore.Name, engine, "success").Inc()
			if d := jobDuration(current); d > 0 {
				RestoreDurationSeconds.WithLabelValues(restore.Namespace, restore.Name, engine, "success").Observe(d)
			}
		case dumpscriptv1alpha1.RestorePhaseFailed:
			log.Info("restore failed", "job", current.Name)
			r.eventf(&restore, corev1.EventTypeWarning, RestoreReasonFailed,
				"restore Job %s failed (attempts=%d)", current.Name, current.Status.Failed)
			RestoreTotal.WithLabelValues(restore.Namespace, restore.Name, engine, "failure").Inc()
			if d := jobDuration(current); d > 0 {
				RestoreDurationSeconds.WithLabelValues(restore.Namespace, restore.Name, engine, "failure").Observe(d)
			}
		}
	}
	return ctrl.Result{}, err
}

// eventf records a Kubernetes Event on the Restore object. No-op when the
// recorder is not wired (unit tests, etc.).
func (r *RestoreReconciler) eventf(restore *dumpscriptv1alpha1.Restore, eventType, reason, format string, args ...interface{}) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(restore, eventType, reason, format, args...)
}

// setRestoreReadyCondition mirrors the BackupSchedule Ready condition pattern:
// True when phase=Succeeded, False when phase=Failed, Unknown otherwise.
func setRestoreReadyCondition(s *dumpscriptv1alpha1.RestoreStatus, generation int64) {
	cond := metav1.Condition{
		Type:               RestoreConditionReady,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
	switch s.Phase {
	case dumpscriptv1alpha1.RestorePhaseSucceeded:
		cond.Status = metav1.ConditionTrue
		cond.Reason = RestoreReasonSucceeded
		cond.Message = s.Message
	case dumpscriptv1alpha1.RestorePhaseFailed:
		cond.Status = metav1.ConditionFalse
		cond.Reason = RestoreReasonFailed
		cond.Message = s.Message
	default:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = RestoreReasonRunning
		cond.Message = "restore is in progress"
	}
	apimeta.SetStatusCondition(&s.Conditions, cond)
}

// lastJobConditionTime returns the LastTransitionTime of the most recent
// matching condition on a Job, or nil when none is found.
func lastJobConditionTime(j *batchv1.Job, t batchv1.JobConditionType) *metav1.Time {
	for i := range j.Status.Conditions {
		c := &j.Status.Conditions[i]
		if c.Type == t && c.Status == corev1.ConditionTrue {
			return &c.LastTransitionTime
		}
	}
	return nil
}

// patchRestoreStatus refetches the Restore CR and applies the mutator to its
// Status, retrying on resource-version conflicts. After success the in-memory
// `dst` is overwritten with the latest version so callers see the final state.
func (r *RestoreReconciler) patchRestoreStatus(
	ctx context.Context,
	dst *dumpscriptv1alpha1.Restore,
	mutate func(*dumpscriptv1alpha1.RestoreStatus),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest dumpscriptv1alpha1.Restore
		if err := r.Get(ctx, client.ObjectKey{Name: dst.Name, Namespace: dst.Namespace}, &latest); err != nil {
			return err
		}
		mutate(&latest.Status)
		if err := r.Status().Update(ctx, &latest); err != nil {
			return err
		}
		*dst = latest
		return nil
	})
}

// SetupWithManager registers the controller and watches owned Jobs so that
// Job completion events re-trigger reconciliation.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dumpscriptv1alpha1.Restore{}).
		Owns(&batchv1.Job{}).
		Named("restore").
		Complete(r)
}
