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

// SignModeHandler implements the SIGN_MODE_LEGACY_AMINO_JSON signing mode.
type SignModeHandler struct {
	fileResolver protodesc.Resolver
	typeResolver protoregistry.MessageTypeResolver
	encoder      Encoder
}

// SignModeHandlerOptions are the options for the SignModeHandler.
type SignModeHandlerOptions struct {
	fileResolver protodesc.Resolver
	typeResolver protoregistry.MessageTypeResolver
	encoder      *Encoder
}

// NewSignModeHandler returns a new SignModeHandler.
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

// Mode implements the Mode method of the SignModeHandler interface.
func (h SignModeHandler) Mode() signingv1beta1.SignMode {
	return signingv1beta1.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
}

// GetSignBytes implements the GetSignBytes method of the SignModeHandler interface.
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

// sortJSON sorts the JSON keys of the given JSON encoded byte slice.
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
