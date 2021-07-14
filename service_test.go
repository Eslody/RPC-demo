package gonrpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	a, b int
}
//值、指针都可作为接收者
func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.a + args.b
	return nil
}

func (f Foo) sum(args Args, reply *int) error {
	*reply = args.a + args.b
	return nil
}

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

func TestService(t *testing.T) {
	var foo Foo
	//如果传入的是值就不能用指针作为方法的接收者
	s := newService(&foo)
	_assert(len(s.method) == 1, "wrong service Method, expect 1, but got %d", len(s.method))
	mType := s.method["Sum"]
	_assert(mType != nil, "wrong Method, Sum shouldn't nil")
}

func TestMethod(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]

	argv := mType.newArgv()
	reply := mType.newReply()
	argv.Set(reflect.ValueOf(Args{a: 1, b: 3}))
	err := s.call(mType, argv, reply)
	_assert(err == nil && *reply.Interface().(*int) == 4 && mType.NumCalls() == 1, "failed to call Foo.Sum")
}


