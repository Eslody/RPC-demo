package gonrpc
//服务注册
import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64	//方法调用次数
}

func (m *methodType) NumCalls() uint64 {
	//为保证原子性，不能返回m.numCalls
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	//argv可能是指针也可能是值
	if m.ArgType.Kind() == reflect.Ptr {
		//Type.Elem()，指向元素的类型
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReply() reflect.Value {
	var reply reflect.Value
	//reply只能是指针,返回一个reply空值的指针
	reply = reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		reply.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		reply.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return reply
}

type service struct {
	name string
	typ reflect.Type
	rcvr reflect.Value //指向接收者的一个指针
	method map[string]*methodType
}

//传进的参数是Foo的指针
func newService(rcvr interface{}) *service {
	s := new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

//注册方法
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i:=0; i<s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		//服务注册失败
		//参数数量不为3，返回值不为1
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		//返回值未实现error接口
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		//参数未导出
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		//第二个参数不为指针
		if replyType.Kind() != reflect.Ptr {
			continue
		}
		s.method[method.Name] = &methodType{
			method: method,
			ArgType: argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

//service调用方法
func (s *service) call(m *methodType, argv, reply reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, reply})
	if errInter := returnValues[0].Interface(); errInter != nil {
		//将error的interface类型转为error
		return errInter.(error)
	}
	return nil
}





