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
		warnDeprecatedEnv(oldName, "use "+newName+" instead")
		return utils.GetEnv(oldName, defaultValue)
	}
	return utils.GetEnv(newName, defaultValue)
}

func GetEnvBoolWithDeprecated(newName string, oldName string, defaultValue bool) (bool, error) {
	if _, ok := os.LookupEnv(newName); ok {
		return utils.GetEnvBool(newName, defaultValue)
	}
	if _, ok := os.LookupEnv(oldName); ok {
		warnDeprecatedEnv(oldName, "use "+newName+" instead")
		return utils.GetEnvBool(oldName, defaultValue)
	}
	return utils.GetEnvBool(newName, defaultValue)
}

func warnDeprecatedEnv(envName string, deprecationNote string) {
	loggerArgs := LoggerArgs{Component: "config"}
	CreateExtCtxWithArgs(context.Background(), &loggerArgs).Logger().Warn(
		"using deprecated env variable, "+deprecationNote,
		"env", envName,
	)
}

func GetDeprecatedEnv(name string, defaultValue string, deprecationNote string) string {
	if _, ok := os.LookupEnv(name); ok {
		warnDeprecatedEnv(name, deprecationNote)
	}
	return utils.GetEnv(name, defaultValue)
}

func GetDeprecatedEnvBool(name string, defaultValue bool, deprecationNote string) (bool, error) {
	if _, ok := os.LookupEnv(name); ok {
		warnDeprecatedEnv(name, deprecationNote)
	}
	return utils.GetEnvBool(name, defaultValue)
}
