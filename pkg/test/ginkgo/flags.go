package ginkgo

import "fmt"

type retryPolicyFlag RetryPolicy

func (r *retryPolicyFlag) String() string {
	return string(*r)
}

func (r *retryPolicyFlag) Set(value string) error {
	switch value {
	case string(RetryPolicyNone), string(RetryPolicyOnce), string(RetryPolicyMulti):
		*r = retryPolicyFlag(value)
		return nil
	default:
		return fmt.Errorf("invalid retry policy: %s (valid options: none, once, multi)", value)
	}
}

func (r *retryPolicyFlag) Type() string {
	return "string"
}
