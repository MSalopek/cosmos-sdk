package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// AccountKeeper defines the expected interface contract the vesting module
// requires for storing accounts.
type AccountKeeper interface {
	SetAccount(sdk.Context, authtypes.AccountI)
}

// BankKeeper defines the expected interface contract the vesting module requires
// for creating vesting accounts with funds.
type BankKeeper interface {
	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error
	SendCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SpendableCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	BlockedAddr(addr sdk.AccAddress) bool
}

// StakingKeeper defines the expected interface contract the vesting module
// requires for finding and changing the delegated tokens, used in clawback.
type StakingKeeper interface {
	BondDenom(ctx sdk.Context) string
	GetDelegatorDelegations(ctx sdk.Context, delegator sdk.AccAddress,
		maxRetrieve uint16) []stakingtypes.Delegation
	GetUnbondingDelegations(ctx sdk.Context, delegator sdk.AccAddress,
		maxRetrieve uint16) []stakingtypes.UnbondingDelegation
	GetValidator(ctx sdk.Context, valAddr sdk.ValAddress) (stakingtypes.Validator, bool)
	TransferUnbonding(ctx sdk.Context, fromAddr, toAddr sdk.AccAddress, valAddr sdk.ValAddress, wantAmt sdk.Int) sdk.Int
	TransferDelegation(ctx sdk.Context, fromAddr, toAddr sdk.AccAddress, valAddr sdk.ValAddress, wantShares sdk.Dec) sdk.Dec
}
