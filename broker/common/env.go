package common

import (
	"context"
	"os"

	"github.com/indexdata/go-utils/utils"
)

func GetEnvWithDeprecated(newName string, oldName string, defaultValue string) string {
	if _, ok := os.LookupEnv(newName); ok {
		return utils.GetEnv(newName, defaultValue)
	}
	if _, ok := os.LookupEnv(oldName); ok {
		warnDeprecatedEnv(oldName, newName)
		return utils.GetEnv(oldName, defaultValue)
	}
	return utils.GetEnv(newName, defaultValue)
}

func GetEnvBoolWithDeprecated(newName string, oldName string, defaultValue bool) (bool, error) {
	if _, ok := os.LookupEnv(newName); ok {
		return utils.GetEnvBool(newName, defaultValue)
	}
	if _, ok := os.LookupEnv(oldName); ok {
		warnDeprecatedEnv(oldName, newName)
		return utils.GetEnvBool(oldName, defaultValue)
	}
	return utils.GetEnvBool(newName, defaultValue)
}

func warnDeprecatedEnv(oldName string, newName string) {
	loggerArgs := LoggerArgs{Component: "config"}
	CreateExtCtxWithArgs(context.Background(), &loggerArgs).Logger().Warn("using deprecated env " + oldName + ", use " + newName + " instead")
}
