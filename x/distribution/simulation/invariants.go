package simulation

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/distribution/types"
)

// AllInvariants runs all invariants of the distribution module
func AllInvariants(d distr.Keeper, stk types.StakingKeeper) sdk.Invariant {
	return func(ctx sdk.Context) error {
		err := CanWithdrawInvariant(d, stk)(ctx)
		if err != nil {
			return err
		}
		err = NonNegativeOutstandingInvariant(d, stk)(ctx)
		if err != nil {
			return err
		}
		err = ReferenceCountInvariant(d, stk)(ctx)
		if err != nil {
			return err
		}
		return nil
	}
}

// NonNegativeOutstandingInvariant checks that outstanding unwithdrawn fees are never negative
func NonNegativeOutstandingInvariant(k distr.Keeper, sk types.StakingKeeper) sdk.Invariant {
	return func(ctx sdk.Context) error {

		var outstanding sdk.DecCoins

		k.IterateValidatorOutstandingRewards(ctx, func(_ sdk.ValAddress, rewards types.ValidatorOutstandingRewards) (stop bool) {
			outstanding = rewards
			if outstanding.IsAnyNegative() {
				return true
			}
			return false
		})

		if outstanding.IsAnyNegative() {
			return fmt.Errorf("negative outstanding coins: %v", outstanding)
		}

		return nil

	}
}

func CanWithdrawForValidatorInvariant(k distr.Keeper, sk types.StakingKeeper, val sdk.Validator) sdk.Invariant {
	return func(ctx sdk.Context) error {
		_ = k.WithdrawValidatorCommission(ctx, val.GetOperator())
		// ugh so slow TODO FIXME
		// iterate over all current delegations, withdraw rewards
		dels := sk.GetAllSDKDelegations(ctx)
		for _, delegation := range dels {
			if delegation.GetValidatorAddr().String() == val.GetOperator().String() {
				_ = k.WithdrawDelegationRewards(ctx, delegation.GetDelegatorAddr(), delegation.GetValidatorAddr())
			}
		}
		remaining := k.GetValidatorOutstandingRewards(ctx, val.GetOperator())
		if len(remaining) > 0 && remaining[0].Amount.LT(sdk.ZeroDec()) {
			return fmt.Errorf("negative remaining coins: %v", remaining)
		}
		return nil
	}
}

// CanWithdrawInvariant checks that current rewards can be completely withdrawn
func CanWithdrawInvariant(k distr.Keeper, sk types.StakingKeeper) sdk.Invariant {
	return func(ctx sdk.Context) error {

		// cache, we don't want to write changes
		ctx, _ = ctx.CacheContext()

		var err error

		// iterate over all validators
		sk.IterateValidators(ctx, func(_ int64, val sdk.Validator) (stop bool) {
			err = CanWithdrawForValidatorInvariant(k, sk, val)(ctx)
			if err != nil {
				return true
			}
			return false
		})

		if err != nil {
			return err
		}

		return nil
	}
}

// ReferenceCountInvariant checks that the number of historical rewards records is correct
func ReferenceCountInvariant(k distr.Keeper, sk types.StakingKeeper) sdk.Invariant {
	return func(ctx sdk.Context) error {

		valCount := uint64(0)
		sk.IterateValidators(ctx, func(_ int64, val sdk.Validator) (stop bool) {
			valCount++
			return false
		})
		dels := sk.GetAllSDKDelegations(ctx)
		slashCount := uint64(0)
		k.IterateValidatorSlashEvents(ctx,
			func(_ sdk.ValAddress, _ uint64, _ types.ValidatorSlashEvent) (stop bool) {
				slashCount++
				return false
			})

		// one record per validator (last tracked period), one record per
		// delegation (previous period), one record per slash (previous period)
		expected := valCount + uint64(len(dels)) + slashCount
		count := k.GetValidatorHistoricalReferenceCount(ctx)

		if count != expected {
			return fmt.Errorf("unexpected number of historical rewards records: "+
				"expected %v (%v vals + %v dels + %v slashes), got %v",
				expected, valCount, len(dels), slashCount, count)
		}

		return nil
	}
}
