/*

  Copyright 2017 Loopring Project Ltd (Loopring Foundation).

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

*/

package extractor

import (
	"fmt"
	"github.com/Loopring/relay-lib/eth/abi"
	"github.com/Loopring/relay-lib/eth/contract"
	ethtyp "github.com/Loopring/relay-lib/eth/types"
	"github.com/Loopring/relay-lib/log"
	"github.com/Loopring/relay-lib/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"math/big"
)

type MethodData struct {
	types.TxInfo
	Method interface{}
	Abi    *abi.ABI
	Id     string
	Name   string
}

func newMethodData(method *abi.Method, cabi *abi.ABI) MethodData {
	var c MethodData

	c.Id = common.ToHex(method.Id())
	c.Name = method.Name
	c.Abi = cabi

	return c
}

// UnpackMethod v should be ptr
func (m MethodData) handleMethod(tx *ethtyp.Transaction, gasUsed, blockTime *big.Int, status types.TxStatus, methodName string) (err error) {
	var event interface{}
	if err = m.beforeUnpack(tx, gasUsed, blockTime, status, methodName); err != nil {
		return
	}
	if err = m.unpack(tx); err != nil {
		return
	}
	if event, err = m.afterUnpack(); err != nil {
		return
	}

	return Emit(m.Name, event)
}

// beforeUnpack full fill method txinfo and set status...
func (m MethodData) beforeUnpack(tx *ethtyp.Transaction, gasUsed, blockTime *big.Int, status types.TxStatus, methodName string) (err error) {
	m.TxInfo = setTxInfo(tx, gasUsed, blockTime, methodName)
	m.TxLogIndex = 0
	m.Status = status

	switch m.Name {
	case contract.METHOD_CANCEL_ORDER:
		if m.DelegateAddress == types.NilAddress {
			err = fmt.Errorf("cancelOrder method cann't get delegate address")
		}
	}

	return err
}

// afterUnpack set special fields in internal event
func (m MethodData) afterUnpack() (event interface{}, err error) {
	switch m.Name {
	case contract.METHOD_SUBMIT_RING:
		event, err = m.fullFillSubmitRing()
	case contract.METHOD_CANCEL_ORDER:
		event, err = m.fullFillCancelOrder()
	case contract.METHOD_CUTOFF_ALL:
		event, err = m.fullFillCutoffAll()
	case contract.METHOD_CUTOFF_PAIR:
		event, err = m.fullFillCutoffPair()
	case contract.METHOD_APPROVE:
		event, err = m.fullFillApprove()
	case contract.METHOD_TRANSFER:
		event, err = m.fullFillTransfer()
	case contract.METHOD_WETH_DEPOSIT:
		event, err = m.fullFillDeposit()
	case contract.METHOD_WETH_WITHDRAWAL:
		event, err = m.fullFillWithdrawal()
	}
	return
}

func (m MethodData) unpack(tx *ethtyp.Transaction) (err error) {
	data := hexutil.MustDecode("0x" + tx.Input[10:])
	err = m.Abi.Unpack(m.Method, m.Name, data, [][]byte{})
	return err
}

func (m MethodData) fullFillSubmitRing() (event *types.SubmitRingMethodEvent, err error) {
	src, ok := m.Method.(*contract.SubmitRingMethodInputs)
	if !ok {
		return nil, fmt.Errorf("submitRing method inputs type error")
	}

	if event, err = src.ConvertDown(); err != nil {
		return event, fmt.Errorf("submitRing method inputs convert error:%s", err.Error())
	}

	// set txinfo for event
	event.TxInfo = m.TxInfo
	if event.Status == types.TX_STATUS_FAILED {
		event.Err = fmt.Errorf("method %s transaction failed", contract.METHOD_SUBMIT_RING)
	}

	// 不需要发送订单到gateway
	//for _, v := range event.OrderList {
	//	v.Hash = v.GenerateHash()
	//	log.Debugf("extractor,tx:%s submitRing method orderHash:%s,owner:%s,tokenS:%s,tokenB:%s,amountS:%s,amountB:%s", event.TxHash.Hex(), v.Hash.Hex(), v.Owner.Hex(), v.TokenS.Hex(), v.TokenB.Hex(), v.AmountS.String(), v.AmountB.String())
	//	eventemitter.Emit(eventemitter.GatewayNewOrder, v)
	//}

	log.Debugf("extractor,tx:%s submitRing method gas:%s, gasprice:%s, status:%s", event.TxHash.Hex(), event.GasUsed.String(), event.GasPrice.String(), types.StatusStr(event.Status))

	return event, nil
}

func (m MethodData) fullFillCancelOrder() (event *types.OrderCancelledEvent, err error) {
	src, ok := m.Method.(*contract.CancelOrderMethod)
	if !ok {
		return nil, fmt.Errorf("cancelOrder method inputs type error")
	}

	order, cancelAmount, _ := src.ConvertDown()
	order.Protocol = m.Protocol
	order.DelegateAddress = m.DelegateAddress
	order.Hash = order.GenerateHash()

	// 发送到txmanager
	tmCancelEvent := &types.OrderCancelledEvent{}
	tmCancelEvent.TxInfo = m.TxInfo
	tmCancelEvent.OrderHash = order.Hash
	tmCancelEvent.AmountCancelled = cancelAmount

	log.Debugf("extractor,tx:%s cancelOrder method order tokenS:%s,tokenB:%s,amountS:%s,amountB:%s", event.TxHash.Hex(), order.TokenS.Hex(), order.TokenB.Hex(), order.AmountS.String(), order.AmountB.String())

	return tmCancelEvent, nil
}

func (m MethodData) fullFillCutoffAll() (event *types.CutoffEvent, err error) {
	src, ok := m.Method.(*contract.CutoffMethod)
	if !ok {
		return nil, fmt.Errorf("cutoffAll method inputs type error")
	}

	event = src.ConvertDown()
	event.TxInfo = m.TxInfo
	event.Owner = event.From
	log.Debugf("extractor,tx:%s cutoff method owner:%s, cutoff:%d, status:%d", event.TxHash.Hex(), event.Owner.Hex(), event.Cutoff.Int64(), event.Status)

	return event, err
}

func (m MethodData) fullFillCutoffPair() (event *types.CutoffPairEvent, err error) {
	src, ok := m.Method.(*contract.CutoffPairMethod)
	if !ok {
		return nil, fmt.Errorf("cutoffPair method inputs type error")
	}

	event = src.ConvertDown()
	event.TxInfo = m.TxInfo
	event.Owner = event.From

	log.Debugf("extractor,tx:%s cutoffpair method owenr:%s, token1:%s, token2:%s, cutoff:%d", event.TxHash.Hex(), event.Owner.Hex(), event.Token1.Hex(), event.Token2.Hex(), event.Cutoff.Int64())

	return
}

func (m MethodData) fullFillApprove() (event *types.ApprovalEvent, err error) {
	src, ok := m.Method.(*contract.ApproveMethod)
	if !ok {
		return nil, fmt.Errorf("approve method inputs type error")
	}

	event = src.ConvertDown()
	event.TxInfo = m.TxInfo
	event.Owner = m.From

	log.Debugf("extractor,tx:%s approve method owner:%s, spender:%s, value:%s", event.TxHash.Hex(), event.Owner.Hex(), event.Spender.Hex(), event.Amount.String())

	return
}

func (m MethodData) fullFillTransfer() (event *types.TransferEvent, err error) {
	src := m.Method.(*contract.TransferMethod)

	event = src.ConvertDown()
	event.Sender = m.From
	event.TxInfo = m.TxInfo

	log.Debugf("extractor,tx:%s transfer method sender:%s, receiver:%s, value:%s", event.TxHash.Hex(), event.Sender.Hex(), event.Receiver.Hex(), event.Amount.String())

	return
}

func (m MethodData) fullFillDeposit() (event *types.WethDepositEvent, err error) {
	event.Dst = m.From
	event.Amount = m.Value
	event.TxInfo = m.TxInfo

	log.Debugf("extractor,tx:%s wethDeposit method from:%s, to:%s, value:%s", event.TxHash.Hex(), event.From.Hex(), event.To.Hex(), event.Amount.String())

	return
}

func (m MethodData) fullFillWithdrawal() (event *types.WethWithdrawalEvent, err error) {
	src, ok := m.Method.(*contract.WethWithdrawalMethod)
	if !ok {
		return nil, fmt.Errorf("wethWithdrawal method inputs type error")
	}

	event = src.ConvertDown()
	event.Src = m.From
	event.TxInfo = m.TxInfo

	log.Debugf("extractor,tx:%s wethWithdrawal method from:%s, to:%s, value:%s", event.TxHash.Hex(), event.From.Hex(), event.To.Hex(), event.Amount.String())

	return
}
