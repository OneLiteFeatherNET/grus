package crdsync

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

var (
	metaGet    = metav1.GetOptions{}
	metaCreate = metav1.CreateOptions{}
	metaDelete = metav1.DeleteOptions{}
)
