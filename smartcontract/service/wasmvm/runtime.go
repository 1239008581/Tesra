/*
 * Copyright (C) 2019 The TesraSupernet Authors
 * This file is part of The TesraSupernet library.
 *
 * The TesraSupernet is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The TesraSupernet is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The TesraSupernet.  If not, see <http://www.gnu.org/licenses/>.
 */
package wasmvm

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/TesraSupernet/Tesra/common"
	"github.com/TesraSupernet/Tesra/common/log"
	"github.com/TesraSupernet/Tesra/core/payload"
	"github.com/TesraSupernet/Tesra/core/types"
	"github.com/TesraSupernet/Tesra/errors"
	"github.com/TesraSupernet/Tesra/smartcontract/event"
	native2 "github.com/TesraSupernet/Tesra/smartcontract/service/native"
	"github.com/TesraSupernet/Tesra/smartcontract/service/native/utils"
	"github.com/TesraSupernet/Tesra/smartcontract/service/util"
	"github.com/TesraSupernet/Tesra/smartcontract/states"
	"github.com/TesraSupernet/Tesra/vm/crossvm_codec"
	neotypes "github.com/TesraSupernet/Tesra/vm/neovm/types"
	"github.com/go-interpreter/wagon/exec"
	"github.com/go-interpreter/wagon/wasm"
	"io"
)

type ContractType byte

const (
	NATIVE_CONTRACT ContractType = iota
	NEOVM_CONTRACT
	WASMVM_CONTRACT
	UNKOWN_CONTRACT
)

type Runtime struct {
	Service    *WasmVmService
	Input      []byte
	Output     []byte
	CallOutPut []byte
}

func TimeStamp(proc *exec.Process) uint64 {
	self := proc.HostData().(*Runtime)
	self.checkGas(TIME_STAMP_GAS)
	return uint64(self.Service.Time)
}

func BlockHeight(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(BLOCK_HEGHT_GAS)
	return self.Service.Height
}

func SelfAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(SELF_ADDRESS_GAS)
	selfaddr := self.Service.ContextRef.CurrentContext().ContractAddress
	_, err := proc.WriteAt(selfaddr[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func Sha256(proc *exec.Process, src uint32, slen uint32, dst uint32) {
	self := proc.HostData().(*Runtime)
	cost := uint64((slen/1024)+1) * SHA256_GAS
	self.checkGas(cost)

	bs, err := ReadWasmMemory(proc, src, slen)
	if err != nil {
		panic(err)
	}

	sh := sha256.New()
	sh.Write(bs[:])
	hash := sh.Sum(nil)

	_, err = proc.WriteAt(hash[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func CallerAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(CALLER_ADDRESS_GAS)
	if self.Service.ContextRef.CallingContext() != nil {
		calleraddr := self.Service.ContextRef.CallingContext().ContractAddress
		_, err := proc.WriteAt(calleraddr[:], int64(dst))
		if err != nil {
			panic(err)
		}
	} else {
		_, err := proc.WriteAt(common.ADDRESS_EMPTY[:], int64(dst))
		if err != nil {
			panic(err)
		}
	}

}

func EntryAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(ENTRY_ADDRESS_GAS)
	entryAddress := self.Service.ContextRef.EntryContext().ContractAddress
	_, err := proc.WriteAt(entryAddress[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func Checkwitness(proc *exec.Process, dst uint32) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(CHECKWITNESS_GAS)
	var addr common.Address
	_, err := proc.ReadAt(addr[:], int64(dst))
	if err != nil {
		panic(err)
	}

	address, err := common.AddressParseFromBytes(addr[:])
	if err != nil {
		panic(err)
	}

	if self.Service.ContextRef.CheckWitness(address) {
		return 1
	}
	return 0
}

func Ret(proc *exec.Process, ptr uint32, len uint32) {
	self := proc.HostData().(*Runtime)
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		panic(err)
	}

	self.Output = bs
	proc.Terminate()
}

func Debug(proc *exec.Process, ptr uint32, len uint32) {
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		//do not panic on debug
		return
	}

	log.Debugf("[WasmContract]Debug:%s\n", bs)
}

func Notify(proc *exec.Process, ptr uint32, l uint32) {
	self := proc.HostData().(*Runtime)
	if l >= neotypes.MAX_NOTIFY_LENGTH {
		panic("notify length over the uplimit")
	}
	bs, err := ReadWasmMemory(proc, ptr, l)
	if err != nil {
		panic(err)
	}
	notify := &event.NotifyEventInfo{ContractAddress: self.Service.ContextRef.CurrentContext().ContractAddress}
	val := crossvm_codec.DeserializeNotify(bs)
	notify.States = val

	notifys := make([]*event.NotifyEventInfo, 1)
	notifys[0] = notify
	self.Service.ContextRef.PushNotifications(notifys)
}

func InputLength(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	return uint32(len(self.Input))
}

func GetInput(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	_, err := proc.WriteAt(self.Input, int64(dst))
	if err != nil {
		panic(err)
	}
}

func CallOutputLength(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	return uint32(len(self.CallOutPut))
}

func GetCallOut(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	_, err := proc.WriteAt(self.CallOutPut, int64(dst))
	if err != nil {
		panic(err)
	}
}

func GetCurrentTxHash(proc *exec.Process, ptr uint32) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(CURRENT_TX_HASH_GAS)

	txhash := self.Service.Tx.Hash()

	length, err := proc.WriteAt(txhash[:], int64(ptr))
	if err != nil {
		panic(err)
	}

	return uint32(length)
}

func RaiseException(proc *exec.Process, ptr uint32, len uint32) {
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		//do not panic on debug
		return
	}

	panic(fmt.Errorf("[RaiseException]Contract RaiseException:%s\n", bs))
}

func CallContract(proc *exec.Process, contractAddr uint32, inputPtr uint32, inputLen uint32) uint32 {
	self := proc.HostData().(*Runtime)

	self.checkGas(CALL_CONTRACT_GAS)
	var contractAddress common.Address
	_, err := proc.ReadAt(contractAddress[:], int64(contractAddr))
	if err != nil {
		panic(err)
	}

	inputs, err := ReadWasmMemory(proc, inputPtr, inputLen)
	if err != nil {
		panic(err)
	}

	contracttype, err := self.getContractType(contractAddress)
	if err != nil {
		panic(err)
	}

	var result []byte

	switch contracttype {
	case NATIVE_CONTRACT:
		source := common.NewZeroCopySource(inputs)
		ver, eof := source.NextByte()
		if eof {
			panic(io.ErrUnexpectedEOF)
		}
		method, _, irregular, eof := source.NextString()
		if irregular {
			panic(common.ErrIrregularData)
		}
		if eof {
			panic(io.ErrUnexpectedEOF)
		}

		args, _, irregular, eof := source.NextVarBytes()
		if irregular {
			panic(common.ErrIrregularData)
		}
		if eof {
			panic(io.ErrUnexpectedEOF)
		}

		contract := states.ContractInvokeParam{
			Version: ver,
			Address: contractAddress,
			Method:  method,
			Args:    args,
		}

		self.checkGas(NATIVE_INVOKE_GAS)
		native := &native2.NativeService{
			CacheDB:     self.Service.CacheDB,
			InvokeParam: contract,
			Tx:          self.Service.Tx,
			Height:      self.Service.Height,
			Time:        self.Service.Time,
			ContextRef:  self.Service.ContextRef,
			ServiceMap:  make(map[string]native2.Handler),
		}

		tmpRes, err := native.Invoke()
		if err != nil {
			panic(errors.NewErr("[nativeInvoke]AppCall failed:" + err.Error()))
		}

		result = tmpRes

	case WASMVM_CONTRACT:
		conParam := states.WasmContractParam{Address: contractAddress, Args: inputs}
		param := common.SerializeToBytes(&conParam)

		newservice, err := self.Service.ContextRef.NewExecuteEngine(param, types.InvokeWasm)
		if err != nil {
			panic(err)
		}

		tmpRes, err := newservice.Invoke()
		if err != nil {
			panic(err)
		}

		result = tmpRes.([]byte)

	case NEOVM_CONTRACT:
		evalstack, err := util.GenerateNeoVMParamEvalStack(inputs)
		if err != nil {
			panic(err)
		}

		neoservice, err := self.Service.ContextRef.NewExecuteEngine([]byte{}, types.InvokeNeo)
		if err != nil {
			panic(err)
		}

		err = util.SetNeoServiceParamAndEngine(contractAddress, neoservice, evalstack)
		if err != nil {
			panic(err)
		}

		tmp, err := neoservice.Invoke()
		if err != nil {
			panic(err)
		}

		if tmp != nil {
			val := tmp.(*neotypes.VmValue)
			source := common.NewZeroCopySink([]byte{byte(crossvm_codec.VERSION)})

			err = neotypes.BuildResultFromNeo(*val, source)
			if err != nil {
				panic(err)
			}
			result = source.Bytes()
		}

	default:
		panic(errors.NewErr("Not a supported contract type"))
	}

	self.CallOutPut = result
	return uint32(len(self.CallOutPut))
}

func NewHostModule() *wasm.Module {
	m := wasm.NewModule()
	paramTypes := make([]wasm.ValueType, 14)
	for i := 0; i < len(paramTypes); i++ {
		paramTypes[i] = wasm.ValueTypeI32
	}

	m.Types = &wasm.SectionTypes{
		Entries: []wasm.FunctionSig{
			//func()uint64    [0]
			{
				Form:        0, // value for the 'func' type constructor
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI64},
			},
			//func()uint32     [1]
			{
				Form:        0, // value for the 'func' type constructor
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32)     [2]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32)uint32  [3]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32)  [4]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32)uint32  [5]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32,uint32,uint32)uint32  [6]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32,uint32)  [7]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
			//func(uint32,uint32)uint32   [8]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32 * 14)uint32   [9]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  paramTypes,
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//funct()   [10]
			{
				Form: 0, // value for the 'func' type constructor
			},
			//func(uint32,uint32,uint32)  [11]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
		},
	}
	m.FunctionIndexSpace = []wasm.Function{
		{ //0
			Sig:  &m.Types.Entries[0],
			Host: reflect.ValueOf(TimeStamp),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //1
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(BlockHeight),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //2
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(InputLength),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //3
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(CallOutputLength),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //4
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(SelfAddress),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //5
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(CallerAddress),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //6
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(EntryAddress),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //7
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(GetInput),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //8
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(GetCallOut),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //9
			Sig:  &m.Types.Entries[3],
			Host: reflect.ValueOf(Checkwitness),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //10
			Sig:  &m.Types.Entries[3],
			Host: reflect.ValueOf(GetCurrentBlockHash),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //11
			Sig:  &m.Types.Entries[3],
			Host: reflect.ValueOf(GetCurrentTxHash),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //12
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Ret),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //13
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Notify),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //14
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Debug),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //15
			Sig:  &m.Types.Entries[5],
			Host: reflect.ValueOf(CallContract),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //16
			Sig:  &m.Types.Entries[6],
			Host: reflect.ValueOf(StorageRead),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //17
			Sig:  &m.Types.Entries[7],
			Host: reflect.ValueOf(StorageWrite),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //18
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(StorageDelete),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //19
			Sig:  &m.Types.Entries[9],
			Host: reflect.ValueOf(ContractCreate),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //20
			Sig:  &m.Types.Entries[9],
			Host: reflect.ValueOf(ContractMigrate),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //21
			Sig:  &m.Types.Entries[10],
			Host: reflect.ValueOf(ContractDestroy),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //22
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(RaiseException),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //23
			Sig:  &m.Types.Entries[11],
			Host: reflect.ValueOf(Sha256),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
	}

	m.Export = &wasm.SectionExports{
		Entries: map[string]wasm.ExportEntry{
			"tesra_timestamp": {
				FieldStr: "tesra_timestamp",
				Kind:     wasm.ExternalFunction,
				Index:    0,
			},
			"tesra_block_height": {
				FieldStr: "tesra_block_height",
				Kind:     wasm.ExternalFunction,
				Index:    1,
			},
			"tesra_input_length": {
				FieldStr: "tesra_input_length",
				Kind:     wasm.ExternalFunction,
				Index:    2,
			},
			"tesra_call_output_length": {
				FieldStr: "tesra_call_output_length",
				Kind:     wasm.ExternalFunction,
				Index:    3,
			},
			"tesra_self_address": {
				FieldStr: "tesra_self_address",
				Kind:     wasm.ExternalFunction,
				Index:    4,
			},
			"tesra_caller_address": {
				FieldStr: "tesra_caller_address",
				Kind:     wasm.ExternalFunction,
				Index:    5,
			},
			"tesra_entry_address": {
				FieldStr: "tesra_entry_address",
				Kind:     wasm.ExternalFunction,
				Index:    6,
			},
			"tesra_get_input": {
				FieldStr: "tesra_get_input",
				Kind:     wasm.ExternalFunction,
				Index:    7,
			},
			"tesra_get_call_output": {
				FieldStr: "tesra_get_call_output",
				Kind:     wasm.ExternalFunction,
				Index:    8,
			},
			"tesra_check_witness": {
				FieldStr: "tesra_check_witness",
				Kind:     wasm.ExternalFunction,
				Index:    9,
			},
			"tesra_current_blockhash": {
				FieldStr: "tesra_current_blockhash",
				Kind:     wasm.ExternalFunction,
				Index:    10,
			},
			"tesra_current_txhash": {
				FieldStr: "tesra_current_txhash",
				Kind:     wasm.ExternalFunction,
				Index:    11,
			},
			"tesra_return": {
				FieldStr: "tesra_return",
				Kind:     wasm.ExternalFunction,
				Index:    12,
			},
			"tesra_notify": {
				FieldStr: "tesra_notify",
				Kind:     wasm.ExternalFunction,
				Index:    13,
			},
			"tesra_debug": {
				FieldStr: "tesra_debug",
				Kind:     wasm.ExternalFunction,
				Index:    14,
			},
			"tesra_call_contract": {
				FieldStr: "tesra_call_contract",
				Kind:     wasm.ExternalFunction,
				Index:    15,
			},
			"tesra_storage_read": {
				FieldStr: "tesra_storage_read",
				Kind:     wasm.ExternalFunction,
				Index:    16,
			},
			"tesra_storage_write": {
				FieldStr: "tesra_storage_write",
				Kind:     wasm.ExternalFunction,
				Index:    17,
			},
			"tesra_storage_delete": {
				FieldStr: "tesra_storage_delete",
				Kind:     wasm.ExternalFunction,
				Index:    18,
			},
			"tesra_contract_create": {
				FieldStr: "tesra_contract_create",
				Kind:     wasm.ExternalFunction,
				Index:    19,
			},
			"tesra_contract_migrate": {
				FieldStr: "tesra_contract_migrate",
				Kind:     wasm.ExternalFunction,
				Index:    20,
			},
			"tesra_contract_destroy": {
				FieldStr: "tesra_contract_destroy",
				Kind:     wasm.ExternalFunction,
				Index:    21,
			},
			"tesra_panic": {
				FieldStr: "tesra_panic",
				Kind:     wasm.ExternalFunction,
				Index:    22,
			},
			"tesra_sha256": {
				FieldStr: "tesra_sha256",
				Kind:     wasm.ExternalFunction,
				Index:    23,
			},
		},
	}

	return m
}

func (self *Runtime) getContractType(addr common.Address) (ContractType, error) {
	if utils.IsNativeContract(addr) {
		return NATIVE_CONTRACT, nil
	}

	dep, err := self.Service.CacheDB.GetContract(addr)
	if err != nil {
		return UNKOWN_CONTRACT, err
	}
	if dep == nil {
		return UNKOWN_CONTRACT, errors.NewErr("contract is not exist.")
	}
	if dep.VmType() == payload.WASMVM_TYPE {
		return WASMVM_CONTRACT, nil
	}

	return NEOVM_CONTRACT, nil

}

func (self *Runtime) checkGas(gaslimit uint64) {
	gas := self.Service.vm.AvaliableGas
	if *gas.GasLimit >= gaslimit {
		*gas.GasLimit -= gaslimit
	} else {
		panic(errors.NewErr("[wasm_Service]Insufficient gas limit"))
	}
}

func serializeStorageKey(contractAddress common.Address, key []byte) []byte {
	bf := new(bytes.Buffer)

	bf.Write(contractAddress[:])
	bf.Write(key)

	return bf.Bytes()
}