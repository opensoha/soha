package runtime

import "testing"

func TestBoundComposeRejectsUnregisteredReplicaShapesBeforeExecution(t *testing.T) {
	binding, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	for _, extra := range []string{"", "    scale: 1\n", "    scale: '1'\n", "    deploy:\n      replicas: 1\n      mode: replicated\n"} {
		if _, err := BindCompose("services:\n  web:\n    image: fixture\n"+extra, binding); err != nil {
			t.Fatal(extra, err)
		}
	}
	for _, extra := range []string{
		"    scale: 2\n", "    scale: 0\n", "    scale: '${REPLICAS}'\n", "    scale: [1]\n", "    scale: {}\n",
		"    deploy:\n      replicas: 2\n", "    deploy:\n      replicas: '${REPLICAS}'\n", "    deploy:\n      mode: global\n", "    deploy: []\n",
	} {
		if _, err := BindCompose("services:\n  web:\n    image: fixture\n"+extra, binding); err == nil {
			t.Fatal("unregistered replica shape accepted", extra)
		}
	}
}
