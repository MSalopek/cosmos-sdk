package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// IsValSetSorted reports whether valset is sorted.
func IsValSetSorted(data []types.Validator, powerReduction math.Int) bool {
	n := len(data)
	for i := n - 1; i > 0; i-- {
		if types.ValidatorsByVotingPower(data).Less(i, i-1, powerReduction) {
			return false
		}
	}
	return true
}

func TestHistoricalInfo(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 50, sdk.NewInt(0))
	addrVals := simapp.ConvertAddrsToValAddrs(addrDels)

	validators := make([]types.Validator, len(addrVals))

	for i, valAddr := range addrVals {
		randomEVMAddress, err := teststaking.RandomEVMAddress()
		require.NoError(t, err)
		validators[i] = teststaking.NewValidator(t, valAddr, PKs[i], sdk.AccAddress(PKs[i].Address()), *randomEVMAddress)
	}

	hi := types.NewHistoricalInfo(ctx.BlockHeader(), validators, app.StakingKeeper.PowerReduction(ctx))
	app.StakingKeeper.SetHistoricalInfo(ctx, 2, &hi)

	recv, found := app.StakingKeeper.GetHistoricalInfo(ctx, 2)
	require.True(t, found, "HistoricalInfo not found after set")
	require.Equal(t, hi, recv, "HistoricalInfo not equal")
	require.True(t, IsValSetSorted(recv.Valset, app.StakingKeeper.PowerReduction(ctx)), "HistoricalInfo validators is not sorted")

	app.StakingKeeper.DeleteHistoricalInfo(ctx, 2)

	recv, found = app.StakingKeeper.GetHistoricalInfo(ctx, 2)
	require.False(t, found, "HistoricalInfo found after delete")
	require.Equal(t, types.HistoricalInfo{}, recv, "HistoricalInfo is not empty")
}

func TestTrackHistoricalInfo(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 50, sdk.NewInt(0))
	addrVals := simapp.ConvertAddrsToValAddrs(addrDels)

	// set historical entries in params to 5
	params := types.DefaultParams()
	params.HistoricalEntries = 5
	app.StakingKeeper.SetParams(ctx, params)

	// set historical info at 5, 4 which should be pruned
	// and check that it has been stored
	h4 := tmproto.Header{
		ChainID: "HelloChain",
		Height:  4,
	}
	h5 := tmproto.Header{
		ChainID: "HelloChain",
		Height:  5,
	}
	randomEVMAddress1, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	randomEVMAddress2, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)

	valSet := []types.Validator{
		teststaking.NewValidator(t, addrVals[0], PKs[0], sdk.AccAddress(PKs[0].Address()), *randomEVMAddress1),
		teststaking.NewValidator(t, addrVals[1], PKs[1], sdk.AccAddress(PKs[1].Address()), *randomEVMAddress2),
	}

	hi4 := types.NewHistoricalInfo(h4, valSet, app.StakingKeeper.PowerReduction(ctx))
	hi5 := types.NewHistoricalInfo(h5, valSet, app.StakingKeeper.PowerReduction(ctx))
	app.StakingKeeper.SetHistoricalInfo(ctx, 4, &hi4)
	app.StakingKeeper.SetHistoricalInfo(ctx, 5, &hi5)
	recv, found := app.StakingKeeper.GetHistoricalInfo(ctx, 4)
	require.True(t, found)
	require.Equal(t, hi4, recv)
	recv, found = app.StakingKeeper.GetHistoricalInfo(ctx, 5)
	require.True(t, found)
	require.Equal(t, hi5, recv)

	// genesis validator
	genesisVals := app.StakingKeeper.GetAllValidators(ctx)
	require.Len(t, genesisVals, 1)

	// Set bonded validators in keeper
	randomEVMAddress3, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	val1 := teststaking.NewValidator(t, addrVals[2], PKs[2], sdk.AccAddress(PKs[2].Address()), *randomEVMAddress3)
	val1.Status = types.Bonded // when not bonded, consensus power is Zero
	val1.Tokens = app.StakingKeeper.TokensFromConsensusPower(ctx, 10)
	app.StakingKeeper.SetValidator(ctx, val1)
	app.StakingKeeper.SetLastValidatorPower(ctx, val1.GetOperator(), 10)
	randomEVMAddress4, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	val2 := teststaking.NewValidator(t, addrVals[3], PKs[3], sdk.AccAddress(PKs[3].Address()), *randomEVMAddress4)
	val1.Status = types.Bonded
	val2.Tokens = app.StakingKeeper.TokensFromConsensusPower(ctx, 80)
	app.StakingKeeper.SetValidator(ctx, val2)
	app.StakingKeeper.SetLastValidatorPower(ctx, val2.GetOperator(), 80)

	vals := []types.Validator{val1, genesisVals[0], val2}
	require.True(t, IsValSetSorted(vals, app.StakingKeeper.PowerReduction(ctx)))

	// Set Header for BeginBlock context
	header := tmproto.Header{
		ChainID: "HelloChain",
		Height:  10,
	}
	ctx = ctx.WithBlockHeader(header)

	app.StakingKeeper.TrackHistoricalInfo(ctx)

	// Check HistoricalInfo at height 10 is persisted
	expected := types.HistoricalInfo{
		Header: header,
		Valset: vals,
	}
	recv, found = app.StakingKeeper.GetHistoricalInfo(ctx, 10)
	require.True(t, found, "GetHistoricalInfo failed after BeginBlock")
	require.Equal(t, expected, recv, "GetHistoricalInfo returned unexpected result")

	// Check HistoricalInfo at height 5, 4 is pruned
	recv, found = app.StakingKeeper.GetHistoricalInfo(ctx, 4)
	require.False(t, found, "GetHistoricalInfo did not prune earlier height")
	require.Equal(t, types.HistoricalInfo{}, recv, "GetHistoricalInfo at height 4 is not empty after prune")
	recv, found = app.StakingKeeper.GetHistoricalInfo(ctx, 5)
	require.False(t, found, "GetHistoricalInfo did not prune first prune height")
	require.Equal(t, types.HistoricalInfo{}, recv, "GetHistoricalInfo at height 5 is not empty after prune")
}

func TestGetAllHistoricalInfo(t *testing.T) {
	_, app, ctx := createTestInput(t)
	// clear historical info
	infos := app.StakingKeeper.GetAllHistoricalInfo(ctx)
	require.Len(t, infos, 1)
	app.StakingKeeper.DeleteHistoricalInfo(ctx, infos[0].Header.Height)

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 50, sdk.NewInt(0))
	addrVals := simapp.ConvertAddrsToValAddrs(addrDels)

	randomEVMAddress1, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	randomEVMAddress2, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)

	valSet := []types.Validator{
		teststaking.NewValidator(t, addrVals[0], PKs[0], sdk.AccAddress(PKs[0].Address()), *randomEVMAddress1),
		teststaking.NewValidator(t, addrVals[1], PKs[1], sdk.AccAddress(PKs[1].Address()), *randomEVMAddress2),
	}

	header1 := tmproto.Header{ChainID: "HelloChain", Height: 10}
	header2 := tmproto.Header{ChainID: "HelloChain", Height: 11}
	header3 := tmproto.Header{ChainID: "HelloChain", Height: 12}

	hist1 := types.HistoricalInfo{Header: header1, Valset: valSet}
	hist2 := types.HistoricalInfo{Header: header2, Valset: valSet}
	hist3 := types.HistoricalInfo{Header: header3, Valset: valSet}

	expHistInfos := []types.HistoricalInfo{hist1, hist2, hist3}

	for i, hi := range expHistInfos {
		app.StakingKeeper.SetHistoricalInfo(ctx, int64(10+i), &hi)
	}

	infos = app.StakingKeeper.GetAllHistoricalInfo(ctx)
	require.Equal(t, expHistInfos, infos)
}
