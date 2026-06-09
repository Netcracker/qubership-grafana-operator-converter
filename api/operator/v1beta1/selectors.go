package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (d *GrafanaDashboard) matchesSelector(s *metav1.LabelSelector) (bool, error) {
	selector, err := metav1.LabelSelectorAsSelector(s)
	if err != nil {
		return false, err
	}

	return selector.Empty() || selector.Matches(labels.Set(d.Labels)), nil
}

// MatchesSelectors check if the dashboard matches at least one of the selectors
func (d *GrafanaDashboard) MatchesSelectors(s []*metav1.LabelSelector) (bool, error) {
	result := false

	for _, selector := range s {
		match, err := d.matchesSelector(selector)
		if err != nil {
			return false, err
		}

		result = result || match
	}

	return result, nil
}

func (d *GrafanaFolder) matchesSelector(s *metav1.LabelSelector) (bool, error) {
	selector, err := metav1.LabelSelectorAsSelector(s)
	if err != nil {
		return false, err
	}

	return selector.Empty() || selector.Matches(labels.Set(d.Labels)), nil
}

// MatchesSelectors Check if the dashboard-folder matches at least one of the selectors
func (d *GrafanaFolder) MatchesSelectors(s []*metav1.LabelSelector) (bool, error) {
	result := false

	for _, selector := range s {
		match, err := d.matchesSelector(selector)
		if err != nil {
			return false, err
		}

		result = result || match
	}

	return result, nil
}

func (d *GrafanaContactPoint) matchesSelector(s *metav1.LabelSelector) (bool, error) {
	selector, err := metav1.LabelSelectorAsSelector(s)
	if err != nil {
		return false, err
	}

	return selector.Empty() || selector.Matches(labels.Set(d.Labels)), nil
}

// MatchesSelectors checks if the contact point matches at least one of the selectors
func (d *GrafanaContactPoint) MatchesSelectors(s []*metav1.LabelSelector) (bool, error) {
	result := false

	for _, selector := range s {
		match, err := d.matchesSelector(selector)
		if err != nil {
			return false, err
		}

		result = result || match
	}

	return result, nil
}
