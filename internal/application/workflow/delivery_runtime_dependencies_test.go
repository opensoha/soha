package workflow

import "testing"

func TestValidateDeliveryRuntimeRequiresRepositoryAndBindings(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	s := New(repo, &stubWorkflowApps{}, nil, nil, nil, nil, nil, nil)
	if err := s.ValidateDeliveryRuntime(); err == nil {
		t.Fatal("missing runtime accepted")
	}
	s.SetDeliveryRuntime(runtime, (*deliveryServiceRuntime)(nil))
	if err := s.ValidateDeliveryRuntime(); err == nil {
		t.Fatal("typed-nil principal reader accepted")
	}
	s.SetDeliveryRuntime(runtime, runtime)
	if err := s.ValidateDeliveryRuntime(); err != nil {
		t.Fatal(err)
	}
	s.repo = &stubWorkflowRepository{}
	if err := s.ValidateDeliveryRuntime(); err == nil {
		t.Fatal("repository without durable batch support accepted")
	}
}
