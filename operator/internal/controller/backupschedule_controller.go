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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dumpscriptv1alpha1 "github.com/cloudscript-technology/dumpscript/operator/api/v1alpha1"
)

// BackupScheduleReconciler reconciles a BackupSchedule object.
type BackupScheduleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Condition types and reasons emitted on BackupSchedule.status.conditions[].
const (
	BSConditionReady         = "Ready"
	BSReasonReconciled       = "Reconciled"
	BSReasonCronJobError     = "CronJobError"
	BSReasonLastRunSucceeded = "LastRunSucceeded"
	BSReasonLastRunFailed    = "LastRunFailed"
)

// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile materialises a BackupSchedule into a managed batch/v1 CronJob,
// reflecting suspend/schedule changes and propagating last-success / failure
// times from the latest Job runs back to the CR status.
func (r *BackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var bs dumpscriptv1alpha1.BackupSchedule
	if err := r.Get(ctx, req.NamespacedName, &bs); err != nil {
		// Resource gone — owner-ref garbage-collects the CronJob.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Build the desired CronJob and reconcile it.
	desired := buildCronJob(&bs)
	if err := controllerutil.SetControllerReference(&bs, desired, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set controller ref: %w", err)
	}

	current := &batchv1.CronJob{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, current)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			r.eventf(&bs, corev1.EventTypeWarning, BSReasonCronJobError, "failed to create CronJob: %v", err)
			return ctrl.Result{}, fmt.Errorf("create cronjob: %w", err)
		}
		log.Info("created cronjob", "name", desired.Name)
		r.eventf(&bs, corev1.EventTypeNormal, BSReasonReconciled, "created CronJob %s", desired.Name)
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get cronjob: %w", err)
	default:
		// Mutate in place — preserves resourceVersion for optimistic concurrency.
		current.Spec = desired.Spec
		current.Labels = desired.Labels
		if err := r.Update(ctx, current); err != nil {
			r.eventf(&bs, corev1.EventTypeWarning, BSReasonCronJobError, "failed to update CronJob: %v", err)
			return ctrl.Result{}, fmt.Errorf("update cronjob: %w", err)
		}
		log.V(1).Info("updated cronjob", "name", desired.Name)
	}

	// Propagate last-success / last-failure from the most recent Job runs and
	// emit events / metrics when terminal-state transitions are observed for
	// the first time.
	if err := r.refreshStatus(ctx, &bs); err != nil {
		log.Error(err, "refresh status")
	}
	return ctrl.Result{}, nil
}

// eventf records a Kubernetes Event on the BackupSchedule object. No-op when
// the recorder is not wired (unit tests, etc.).
func (r *BackupScheduleReconciler) eventf(bs *dumpscriptv1alpha1.BackupSchedule, eventType, reason, format string, args ...interface{}) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(bs, eventType, reason, format, args...)
}

// refreshStatus walks the Jobs owned by the managed CronJob and populates
// LastScheduleTime / LastSuccessTime / LastFailureTime / LastRetentionTime +
// CurrentRun, plus the Ready condition. When a terminal-state transition is
// observed for the first time (i.e., the time field moves forward), the
// reconciler emits a Kubernetes Event and bumps the corresponding metric.
//
// Uses retry-on-conflict to handle the race when another reconcile (or a
// kubectl patch) modifies the BackupSchedule between our Get and Status Update.
// Each attempt refetches the latest version so the patch applies cleanly.
func (r *BackupScheduleReconciler) refreshStatus(ctx context.Context, bs *dumpscriptv1alpha1.BackupSchedule) error {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs,
		client.InNamespace(bs.Namespace),
		client.MatchingLabels{"dumpscript.cloudscript.com.br/schedule": bs.Name},
	); err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	// Capture the previous terminal-state pointers so we can detect "moved
	// forward" transitions after the patch and emit events/metrics exactly once.
	prevSuccess := bs.Status.LastSuccessTime
	prevFailure := bs.Status.LastFailureTime

	var (
		newSuccess *metav1.Time
		newFailure *metav1.Time
		successJob *batchv1.Job
		failureJob *batchv1.Job
	)

	// Aggregate metrics derived from the Job list. Computed outside the retry
	// loop because they're functions of the list, not the BackupSchedule.
	var (
		totalRuns           int64
		consecutiveFailures int32
		lastTerminatedJob   *batchv1.Job
	)
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if j.Status.CompletionTime != nil || j.Status.Failed > 0 {
			totalRuns++
			if lastTerminatedJob == nil || jobTerminationTime(j).Time.After(jobTerminationTime(lastTerminatedJob).Time) {
				lastTerminatedJob = j
			}
		}
	}
	consecutiveFailures = countConsecutiveFailures(jobs.Items)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest dumpscriptv1alpha1.BackupSchedule
		if err := r.Get(ctx, client.ObjectKey{Name: bs.Name, Namespace: bs.Namespace}, &latest); err != nil {
			return err
		}

		latest.Status.CurrentRun = ""
		latest.Status.ObservedGeneration = latest.Generation
		latest.Status.TotalRuns = totalRuns
		latest.Status.ConsecutiveFailures = consecutiveFailures
		if lastTerminatedJob != nil {
			latest.Status.LastJobName = lastTerminatedJob.Name
			latest.Status.LastDurationSeconds = int64(jobDuration(lastTerminatedJob))
		}
		for i := range jobs.Items {
			j := &jobs.Items[i]
			switch {
			case j.Status.CompletionTime != nil &&
				(latest.Status.LastSuccessTime == nil || j.Status.CompletionTime.After(latest.Status.LastSuccessTime.Time)):
				latest.Status.LastSuccessTime = j.Status.CompletionTime
				successJob = j
			case j.Status.Failed > 0 &&
				(latest.Status.LastFailureTime == nil || j.CreationTimestamp.After(latest.Status.LastFailureTime.Time)):
				t := j.CreationTimestamp
				latest.Status.LastFailureTime = &t
				failureJob = j
			case j.Status.Active > 0:
				latest.Status.CurrentRun = j.Name
			}
			if latest.Status.LastScheduleTime == nil ||
				j.CreationTimestamp.After(latest.Status.LastScheduleTime.Time) {
				t := j.CreationTimestamp
				latest.Status.LastScheduleTime = &t
			}
		}

		// LastRetentionTime — best-effort: when retention is configured the
		// dumpscript binary runs the prune as part of every successful run, so
		// we mirror LastSuccessTime here. Pruning failures aren't tracked
		// (the binary's logs are authoritative).
		if latest.Spec.RetentionDays > 0 {
			latest.Status.LastRetentionTime = latest.Status.LastSuccessTime
		}

		// Ready condition — Healthy when the most recent terminal state was
		// success, Degraded when failure was more recent, Unknown when neither.
		setReadyCondition(&latest)

		newSuccess = latest.Status.LastSuccessTime
		newFailure = latest.Status.LastFailureTime
		return r.Status().Update(ctx, &latest)
	})
	if err != nil {
		return err
	}

	// Emit events + metrics for first-time observed transitions.
	engine := bs.Spec.Database.Type
	if successJob != nil && (prevSuccess == nil || newSuccess.After(prevSuccess.Time)) {
		r.eventf(bs, corev1.EventTypeNormal, BSReasonLastRunSucceeded,
			"backup job %s completed successfully", successJob.Name)
		BackupTotal.WithLabelValues(bs.Namespace, bs.Name, engine, "success").Inc()
		if d := jobDuration(successJob); d > 0 {
			BackupDurationSeconds.WithLabelValues(bs.Namespace, bs.Name, engine, "success").Observe(d)
		}
	}
	if failureJob != nil && (prevFailure == nil || newFailure.After(prevFailure.Time)) {
		r.eventf(bs, corev1.EventTypeWarning, BSReasonLastRunFailed,
			"backup job %s failed (attempts=%d)", failureJob.Name, failureJob.Status.Failed)
		BackupTotal.WithLabelValues(bs.Namespace, bs.Name, engine, "failure").Inc()
		if d := jobDuration(failureJob); d > 0 {
			BackupDurationSeconds.WithLabelValues(bs.Namespace, bs.Name, engine, "failure").Observe(d)
		}
	}
	return nil
}

// setReadyCondition installs/updates the Ready condition on a BackupSchedule
// based on the relative ordering of LastSuccessTime and LastFailureTime.
func setReadyCondition(bs *dumpscriptv1alpha1.BackupSchedule) {
	cond := metav1.Condition{
		Type:               BSConditionReady,
		ObservedGeneration: bs.Generation,
		LastTransitionTime: metav1.Now(),
	}
	switch {
	case bs.Status.LastSuccessTime == nil && bs.Status.LastFailureTime == nil:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = BSReasonReconciled
		cond.Message = "no runs observed yet"
	case bs.Status.LastFailureTime != nil &&
		(bs.Status.LastSuccessTime == nil || bs.Status.LastFailureTime.After(bs.Status.LastSuccessTime.Time)):
		cond.Status = metav1.ConditionFalse
		cond.Reason = BSReasonLastRunFailed
		cond.Message = "most recent run failed; see Job logs for details"
	default:
		cond.Status = metav1.ConditionTrue
		cond.Reason = BSReasonLastRunSucceeded
		cond.Message = "most recent run succeeded"
	}
	apimeta.SetStatusCondition(&bs.Status.Conditions, cond)
}

// jobTerminationTime returns the wall-clock time at which a Job moved into a
// terminal state — completion for success, the failed-condition time for
// failure, falling back to the Job's CreationTimestamp when neither is yet
// recorded. Used to find the most recent terminated Job in a list.
func jobTerminationTime(j *batchv1.Job) metav1.Time {
	if j.Status.CompletionTime != nil {
		return *j.Status.CompletionTime
	}
	if t := lastJobConditionTime(j, batchv1.JobFailed); t != nil {
		return *t
	}
	return j.CreationTimestamp
}

// countConsecutiveFailures walks the Jobs sorted by their termination time
// (descending) and counts failures up to the first observed success. The
// counter resets to 0 the moment a successful Job appears in the timeline.
func countConsecutiveFailures(jobs []batchv1.Job) int32 {
	// Collect only terminated jobs so transient Active state doesn't shift
	// the count.
	terminated := make([]batchv1.Job, 0, len(jobs))
	for i := range jobs {
		if jobs[i].Status.CompletionTime != nil || jobs[i].Status.Failed > 0 {
			terminated = append(terminated, jobs[i])
		}
	}
	// Sort by termination time, newest first.
	for i := 1; i < len(terminated); i++ {
		for j := i; j > 0 && jobTerminationTime(&terminated[j]).Time.After(jobTerminationTime(&terminated[j-1]).Time); j-- {
			terminated[j], terminated[j-1] = terminated[j-1], terminated[j]
		}
	}
	var n int32
	for i := range terminated {
		if terminated[i].Status.Succeeded > 0 {
			break
		}
		if terminated[i].Status.Failed > 0 {
			n++
		}
	}
	return n
}

// jobDuration returns the seconds elapsed between Job start and completion,
// or 0 when those timestamps are not yet populated.
func jobDuration(j *batchv1.Job) float64 {
	if j == nil || j.Status.StartTime == nil {
		return 0
	}
	end := j.Status.CompletionTime
	if end == nil {
		// Failed Jobs may not have CompletionTime set; fall back to the
		// CreationTimestamp of the latest pod-failure condition's transition,
		// which we approximate with the Job's most recent condition.
		for i := range j.Status.Conditions {
			c := &j.Status.Conditions[i]
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				end = &c.LastTransitionTime
				break
			}
		}
	}
	if end == nil {
		return 0
	}
	return end.Sub(j.Status.StartTime.Time).Seconds()
}

// SetupWithManager sets up the controller with the Manager.
// Owns CronJobs so spec/status changes re-trigger Reconcile.
// Also watches Jobs labelled with the schedule name so that Job
// completion events update LastSuccessTime / LastFailureTime promptly.
func (r *BackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dumpscriptv1alpha1.BackupSchedule{}).
		Owns(&batchv1.CronJob{}).
		Watches(
			&batchv1.Job{},
			handler.EnqueueRequestsFromMapFunc(r.jobToSchedule),
		).
		Named("backupschedule").
		Complete(r)
}

// jobToSchedule maps a Job back to its owning BackupSchedule using the
// "dumpscript.cloudscript.com.br/schedule" label set by buildCronJob.
func (r *BackupScheduleReconciler) jobToSchedule(_ context.Context, obj client.Object) []reconcile.Request {
	name, ok := obj.GetLabels()["dumpscript.cloudscript.com.br/schedule"]
	if !ok {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: obj.GetNamespace()},
	}}
}
