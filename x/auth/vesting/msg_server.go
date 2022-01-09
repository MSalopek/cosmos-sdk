package vesting

import (
	"context"
	"fmt"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/armon/go-metrics"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// msgServer holds the state to serve vesting messages.
type msgServer struct {
	keeper.AccountKeeper
	types.BankKeeper
	types.StakingKeeper
}

// NewMsgServerImpl returns an implementation of the vesting MsgServer interface,
// wrapping the corresponding keepers.
func NewMsgServerImpl(k keeper.AccountKeeper, bk types.BankKeeper, sk types.StakingKeeper) types.MsgServer {
	return &msgServer{AccountKeeper: k, BankKeeper: bk, StakingKeeper: sk}
}

var _ types.MsgServer = msgServer{}

// CreateVestingAccount creates a new delayed or continuous vesting account.
func (s msgServer) CreateVestingAccount(goCtx context.Context, msg *types.MsgCreateVestingAccount) (*types.MsgCreateVestingAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ak := s.AccountKeeper
	bk := s.BankKeeper

	if err := bk.IsSendEnabledCoins(ctx, msg.Amount...); err != nil {
		return nil, err
	}

	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, err
	}
	to, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, err
	}

	if bk.BlockedAddr(to) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", msg.ToAddress)
	}

	if acc := ak.GetAccount(ctx, to); acc != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists", msg.ToAddress)
	}

	baseAccount := ak.NewAccountWithAddress(ctx, to)
	if _, ok := baseAccount.(*authtypes.BaseAccount); !ok {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid account type; expected: BaseAccount, got: %T", baseAccount)
	}

	baseVestingAccount := types.NewBaseVestingAccount(baseAccount.(*authtypes.BaseAccount), msg.Amount.Sort(), msg.EndTime)

	var acc authtypes.AccountI

	if msg.Delayed {
		acc = types.NewDelayedVestingAccountRaw(baseVestingAccount)
	} else {
		acc = types.NewContinuousVestingAccountRaw(baseVestingAccount, ctx.BlockTime().Unix())
	}

	ak.SetAccount(ctx, acc)

	defer func() {
		telemetry.IncrCounter(1, "new", "account")

		for _, a := range msg.Amount {
			if a.Amount.IsInt64() {
				telemetry.SetGaugeWithLabels(
					[]string{"tx", "msg", "create_vesting_account"},
					float32(a.Amount.Int64()),
					[]metrics.Label{telemetry.NewLabel("denom", a.Denom)},
				)
			}
		}
	}()

	err = bk.SendCoins(ctx, from, to, msg.Amount)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	)

	return &types.MsgCreateVestingAccountResponse{}, nil
}

// CreatePeriodicVestingAccount creates a new periodic vesting account, or merges a grant into an existing one.
func (s msgServer) CreatePeriodicVestingAccount(goCtx context.Context, msg *types.MsgCreatePeriodicVestingAccount) (*types.MsgCreatePeriodicVestingAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	ak := s.AccountKeeper
	bk := s.BankKeeper

	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, err
	}
	to, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, err
	}

	if bk.BlockedAddr(to) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", msg.ToAddress)
	}

	var totalCoins sdk.Coins
	for _, period := range msg.VestingPeriods {
		totalCoins = totalCoins.Add(period.Amount...)
	}
	totalCoins = totalCoins.Sort()

	madeNewAcc := false
	acc := ak.GetAccount(ctx, to)

	if acc != nil {
		pva, ok := acc.(*types.PeriodicVestingAccount)
		if !msg.Merge {
			if ok {
				return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists; consider using --merge", msg.ToAddress)
			}
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists", msg.ToAddress)
		}
		if !ok {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "account %s must be a periodic vesting account", msg.ToAddress)
		}
		newStart, newEnd, newPeriods := types.DisjunctPeriods(pva.StartTime, msg.GetStartTime(),
			pva.GetVestingPeriods(), msg.GetVestingPeriods())
		pva.StartTime = newStart
		pva.EndTime = newEnd
		pva.VestingPeriods = newPeriods
		pva.OriginalVesting = pva.OriginalVesting.Add(totalCoins...)
	} else {
		baseAccount := ak.NewAccountWithAddress(ctx, to)
		acc = types.NewPeriodicVestingAccount(baseAccount.(*authtypes.BaseAccount), totalCoins, msg.StartTime, msg.VestingPeriods)
		madeNewAcc = true
	}

	ak.SetAccount(ctx, acc)

	if madeNewAcc {
		defer func() {
			telemetry.IncrCounter(1, "new", "account")

			for _, a := range totalCoins {
				if a.Amount.IsInt64() {
					telemetry.SetGaugeWithLabels(
						[]string{"tx", "msg", "create_periodic_vesting_account"},
						float32(a.Amount.Int64()),
						[]metrics.Label{telemetry.NewLabel("denom", a.Denom)},
					)
				}
			}
		}()
	}

	err = bk.SendCoins(ctx, from, to, totalCoins)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	)
	return &types.MsgCreatePeriodicVestingAccountResponse{}, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// CreateTrueVestingAccount creates a new "true" vesting account, or merges a grant into an existing one.
func (s msgServer) CreateTrueVestingAccount(goCtx context.Context, msg *types.MsgCreateTrueVestingAccount) (*types.MsgCreateTrueVestingAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ak := s.AccountKeeper
	bk := s.BankKeeper

	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, err
	}
	to, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, err
	}

	if bk.BlockedAddr(to) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", msg.ToAddress)
	}

	vestingCoins := sdk.NewCoins()
	for _, period := range msg.VestingPeriods {
		vestingCoins = vestingCoins.Add(period.Amount...)
	}

	lockupCoins := sdk.NewCoins()
	for _, period := range msg.LockupPeriods {
		lockupCoins = lockupCoins.Add(period.Amount...)
	}

	if !vestingCoins.IsZero() && len(msg.LockupPeriods) == 0 {
		// If lockup absent, default to an instant unlock schedule
		msg.LockupPeriods = []types.Period{
			{Length: 0, Amount: vestingCoins},
		}
		lockupCoins = vestingCoins
	}

	if !lockupCoins.IsZero() && len(msg.VestingPeriods) == 0 {
		// If vesting absent, default to an instant vesting schedule
		msg.VestingPeriods = []types.Period{
			{Length: 0, Amount: lockupCoins},
		}
		vestingCoins = lockupCoins
	}

	if !vestingCoins.IsEqual(lockupCoins) { // XXX IsEqual can crash
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "lockup and vesting amounts must be equal")
	}

	madeNewAcc := false
	acc := ak.GetAccount(ctx, to)

	if acc != nil {
		pva, ok := acc.(*types.TrueVestingAccount)
		if !msg.Merge {
			if ok {
				return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists; consider using --merge", msg.ToAddress)
			}
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists", msg.ToAddress)
		}
		if !ok {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "account %s must be a true vesting account", msg.ToAddress)
		}
		if msg.FromAddress != pva.FunderAddress {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s can only accept grants from account %s", msg.ToAddress, pva.FunderAddress)
		}
		newStart, newEnd, newLockupPeriods := types.DisjunctPeriods(pva.StartTime, msg.GetStartTime(), pva.LockupPeriods, msg.LockupPeriods)
		newStartX, newEndX, newVestingPeriods := types.DisjunctPeriods(pva.StartTime, msg.GetStartTime(),
			pva.GetVestingPeriods(), msg.GetVestingPeriods())
		if newStart != newStartX {
			panic("bad start time calculation")
		}
		pva.StartTime = newStart
		pva.EndTime = max64(newEnd, newEndX)
		pva.LockupPeriods = newLockupPeriods
		pva.VestingPeriods = newVestingPeriods
		pva.OriginalVesting = pva.OriginalVesting.Add(vestingCoins...)
	} else {
		baseAccount := ak.NewAccountWithAddress(ctx, to)
		acc = types.NewTrueVestingAccount(baseAccount.(*authtypes.BaseAccount), vestingCoins, msg.StartTime, msg.LockupPeriods, msg.VestingPeriods)
		madeNewAcc = true
	}

	ak.SetAccount(ctx, acc)

	if madeNewAcc {
		defer func() {
			telemetry.IncrCounter(1, "new", "account")

			for _, a := range vestingCoins {
				if a.Amount.IsInt64() {
					telemetry.SetGaugeWithLabels(
						[]string{"tx", "msg", "create_true_vesting_account"},
						float32(a.Amount.Int64()),
						[]metrics.Label{telemetry.NewLabel("denom", a.Denom)},
					)
				}
			}
		}()
	}

	err = bk.SendCoins(ctx, from, to, vestingCoins)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	)

	return &types.MsgCreateTrueVestingAccountResponse{}, nil
}

// Clawback removes the unvested amount from a TrueVestingAccount.
// The destination defaults to the funder address, but
func (s msgServer) Clawback(goCtx context.Context, msg *types.MsgClawback) (*types.MsgClawbackResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	ak := s.AccountKeeper
	bk := s.BankKeeper

	funder, err := sdk.AccAddressFromBech32(msg.GetFunderAddress())
	if err != nil {
		return nil, err
	}
	addr, err := sdk.AccAddressFromBech32(msg.GetAddress())
	if err != nil {
		return nil, err
	}
	dest := funder
	if msg.GetDestAddress() != "" {
		dest, err = sdk.AccAddressFromBech32(msg.GetDestAddress())
		if err != nil {
			return nil, err
		}
	}

	if bk.BlockedAddr(dest) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", msg.DestAddress)
	}

	acc := ak.GetAccount(ctx, addr)
	tva, ok := acc.(*types.TrueVestingAccount)
	if !ok {
		return nil, fmt.Errorf("account not subject to clawback: %s", msg.Address)
	}

	if tva.FunderAddress != msg.GetFunderAddress() {
		return nil, fmt.Errorf("clawback can only be requested by original funder %s", tva.FunderAddress)
	}

	err = tva.Clawback(ctx, dest, ak, bk, s.StakingKeeper)
	if err != nil {
		return nil, err
	}

	return &types.MsgClawbackResponse{}, nil
}
