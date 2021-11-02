package auth

import (
	"fmt"

	"github.com/casbin/casbin/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Casbin wrapper

type Authorizer struct {
	enforcer *casbin.Enforcer
}

// New returns an instance of Authorizer. The `model` and `policy` arguments are
// paths to the files where you've defined the model (which will configure
// Casbin's authorization mechanism—which for us will be
//  ACL) and the policy(which is a CSV file containing your ACL table).
func New(model, policy string) (*Authorizer, error) {
	enforcer, err := casbin.NewEnforcer(model, policy)
	if err != nil {
		return nil, err
	}
	return &Authorizer{
		enforcer: enforcer,
	}, nil
}

// Authorize defers to Casbin's `Enforce` function.
func (a *Authorizer) Authorize(subject, object, action string) error {
	// `Enforce` function returns whether the given subject is permitted to run
	// the given action on the given object based on the model and policy we
	// configure Casbin with.
	ok, err := a.enforcer.Enforce(subject, object, action)
	if err != nil {
		return err
	}
	if !ok {
		msg := fmt.Sprintf("%s not permitted to %s to %s", subject, action, object)
		st := status.New(codes.PermissionDenied, msg)
		return st.Err()
	}
	return nil
}
