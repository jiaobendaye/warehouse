package service

import (
	"fmt"
	"log"
)

// oplog is the shared operation logger used by all services.
var oplog = log.Default()

// logOp writes a structured operation log line in the format:
//
//	[component] operation key=value key=value ...
//
// Example:
//
//	logOp("stock", "inbound", "accessory_id", 42, "name", "充电器", "qty", 10, "balance_after", 100, "client_ref", "abc")
func logOp(component, op string, kv ...any) {
	msg := fmt.Sprintf("[%s] %s", component, op)
	for i := 0; i+1 < len(kv); i += 2 {
		msg += fmt.Sprintf(" %v=%v", kv[i], kv[i+1])
	}
	oplog.Print(msg)
}
