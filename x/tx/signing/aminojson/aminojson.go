package aminojson

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	"cosmossdk.io/x/tx/decode"
	"cosmossdk.io/x/tx/signing"
)

type SignModeHandler struct {
	fileResolver protodesc.Resolver
	typeResolver protoregistry.MessageTypeResolver
	encoder      Encoder
}

type SignModeHandlerOptions struct {
	fileResolver protodesc.Resolver
	typeResolver protoregistry.MessageTypeResolver
	encoder      *Encoder
}

func NewSignModeHandler(options SignModeHandlerOptions) *SignModeHandler {
	h := &SignModeHandler{}
	if options.fileResolver == nil {
		h.fileResolver = protoregistry.GlobalFiles
	} else {
		h.fileResolver = options.fileResolver
	}
	if options.typeResolver == nil {
		h.typeResolver = protoregistry.GlobalTypes
	} else {
		h.typeResolver = options.typeResolver
	}
	if options.encoder == nil {
		h.encoder = NewAminoJSON()
	} else {
		h.encoder = *options.encoder
	}
	return h
}

func (h SignModeHandler) Mode() signingv1beta1.SignMode {
	return signingv1beta1.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
}

func (h SignModeHandler) GetSignBytes(_ context.Context, signerData signing.SignerData, txData signing.TxData) ([]byte, error) {
	body := txData.Body
	_, err := decode.RejectUnknownFields(
		txData.BodyBytes, body.ProtoReflect().Descriptor(), false, h.fileResolver)
	if err != nil {
		return nil, err
	}

	if (len(body.ExtensionOptions) > 0) || (len(body.NonCriticalExtensionOptions) > 0) {
		return nil, fmt.Errorf("%s does not support protobuf extension options: invalid request", h.Mode())
	}

	if signerData.Address == "" {
		return nil, fmt.Errorf("got empty address in %s handler: invalid request", h.Mode())
	}

	tip := txData.AuthInfo.Tip
	if tip != nil && tip.Tipper == "" {
		return nil, fmt.Errorf("tipper cannot be empty")
	}
	isTipper := tip != nil && tip.Tipper == signerData.Address

	var fee *txv1beta1.AminoSignFee
	if isTipper {
		fee = &txv1beta1.AminoSignFee{
			Amount: nil,
			Gas:    0,
		}
	} else {
		f := txData.AuthInfo.Fee
		if f == nil {
			return nil, fmt.Errorf("fee cannot be nil when tipper is not signer")
		}
		fee = &txv1beta1.AminoSignFee{
			Amount:  f.Amount,
			Gas:     f.GasLimit,
			Payer:   f.Payer,
			Granter: f.Granter,
		}
	}

	//msgBytes := make([][]byte, len(txData.Body.Messages))
	//for _, anyMsg := range txData.Body.Messages {
	//	msg, err := anyutil.Unpack(anyMsg, h.fileResolver, h.typeResolver)
	//	if err != nil {
	//		return nil, err
	//	}
	//	bz, err := h.encoder.Marshal(msg)
	//	if err != nil {
	//		return nil, err
	//	}
	//	msgBytes = append(msgBytes, bz)
	//}

	//feeBz, err := h.encoder.Marshal(fee)
	//if err != nil {
	//	return nil, err
	//}

	signDoc := &txv1beta1.AminoSignDoc{
		AccountNumber: signerData.AccountNumber,
		TimeoutHeight: body.TimeoutHeight,
		ChainId:       signerData.ChainId,
		Sequence:      signerData.Sequence,
		Memo:          body.Memo,
		Msgs:          txData.Body.Messages,
		Fee:           fee,
	}

	bz, err := h.encoder.Marshal(signDoc)
	if err != nil {
		return nil, err
	}
	return sortJSON(bz)
}

func sortJSON(toSortJSON []byte) ([]byte, error) {
	var c interface{}
	err := json.Unmarshal(toSortJSON, &c)
	if err != nil {
		return nil, err
	}
	js, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return js, nil
}

var _ signing.SignModeHandler = (*SignModeHandler)(nil)
