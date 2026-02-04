package prservice

import (
	"embed"
	"encoding/json"

	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
)

type StateModelService struct {
	stateMap map[string]*proapi.StateModel
}

func (s *StateModelService) GetStateModel(modelName string) *proapi.StateModel {
	if s.stateMap == nil {
		s.stateMap = make(map[string]*proapi.StateModel)
	}

	stateModel, ok := s.stateMap[modelName]

	if ok {
		return stateModel
	}

	stateModel, err := LoadStateModelByName(modelName)
	if err == nil {
		s.stateMap[modelName] = stateModel
		return stateModel
	}
	return nil
}

//go:embed statemodels
var modelFS embed.FS

func LoadStateModelByName(modelName string) (*proapi.StateModel, error) {
	path := "statemodels/" + modelName + ".json"
	stateModel, err := loadStateModel(path)
	if err != nil {
		return nil, err
	}
	return stateModel, nil
}

func loadStateModel(path string) (*proapi.StateModel, error) {
	data, err := modelFS.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var stateModel proapi.StateModel
	err = json.Unmarshal(data, &stateModel)

	return &stateModel, err
}
