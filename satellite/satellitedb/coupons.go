// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	"storj.io/common/memory"
	"storj.io/storj/private/dbutil"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/payments"
	"storj.io/storj/satellite/payments/coinpayments"
	"storj.io/storj/satellite/payments/stripecoinpayments"
	"storj.io/storj/satellite/satellitedb/dbx"
)

// ensures that coupons implements payments.CouponsDB.
var _ stripecoinpayments.CouponsDB = (*coupons)(nil)

// coupons is an implementation of payments.CouponsDB.
//
// architecture: Database
type coupons struct {
	db *satelliteDB
}

// Insert inserts a coupon into the database.
func (coupons *coupons) Insert(ctx context.Context, coupon payments.Coupon) (err error) {
	defer mon.Task()(&ctx, coupon)(&err)

	id, err := uuid.New()
	if err != nil {
		return err
	}

	_, err = coupons.db.Create_Coupon(
		ctx,
		dbx.Coupon_Id(id[:]),
		dbx.Coupon_ProjectId(coupon.ProjectID[:]),
		dbx.Coupon_UserId(coupon.UserID[:]),
		dbx.Coupon_Amount(coupon.Amount),
		dbx.Coupon_Description(coupon.Description),
		dbx.Coupon_Type(int(coupon.Type)),
		dbx.Coupon_Status(int(coupon.Status)),
		dbx.Coupon_Duration(int64(coupon.Duration)),
	)

	return err
}

// Update updates coupon in database.
func (coupons *coupons) Update(ctx context.Context, couponID uuid.UUID, status payments.CouponStatus) (err error) {
	defer mon.Task()(&ctx, couponID)(&err)

	_, err = coupons.db.Update_Coupon_By_Id(
		ctx,
		dbx.Coupon_Id(couponID[:]),
		dbx.Coupon_Update_Fields{
			Status: dbx.Coupon_Status(int(status)),
		},
	)

	return err
}

// Get returns coupon by ID.
func (coupons *coupons) Get(ctx context.Context, couponID uuid.UUID) (_ payments.Coupon, err error) {
	defer mon.Task()(&ctx, couponID)(&err)

	dbxCoupon, err := coupons.db.Get_Coupon_By_Id(ctx, dbx.Coupon_Id(couponID[:]))
	if err != nil {
		return payments.Coupon{}, err
	}

	return fromDBXCoupon(dbxCoupon)
}

// List returns all coupons of specified user.
func (coupons *coupons) ListByUserID(ctx context.Context, userID uuid.UUID) (_ []payments.Coupon, err error) {
	defer mon.Task()(&ctx, userID)(&err)

	dbxCoupons, err := coupons.db.All_Coupon_By_UserId_OrderBy_Desc_CreatedAt(
		ctx,
		dbx.Coupon_UserId(userID[:]),
	)
	if err != nil {
		return nil, err
	}

	return couponsFromDbxSlice(dbxCoupons)
}

// ListByUserIDAndStatus returns all coupons of specified user and status.
func (coupons *coupons) ListByUserIDAndStatus(ctx context.Context, userID uuid.UUID, status payments.CouponStatus) (_ []payments.Coupon, err error) {
	defer mon.Task()(&ctx, userID)(&err)

	dbxCoupons, err := coupons.db.All_Coupon_By_UserId_And_Status_OrderBy_Desc_CreatedAt(
		ctx,
		dbx.Coupon_UserId(userID[:]),
		dbx.Coupon_Status(int(status)),
	)
	if err != nil {
		return nil, err
	}

	return couponsFromDbxSlice(dbxCoupons)
}

// List returns all coupons with specified status.
func (coupons *coupons) List(ctx context.Context, status payments.CouponStatus) (_ []payments.Coupon, err error) {
	defer mon.Task()(&ctx, status)(&err)

	dbxCoupons, err := coupons.db.All_Coupon_By_Status_OrderBy_Desc_CreatedAt(
		ctx,
		dbx.Coupon_Status(int(status)),
	)
	if err != nil {
		return nil, err
	}

	return couponsFromDbxSlice(dbxCoupons)
}

// ListPending returns paginated list of pending transactions.
func (coupons *coupons) ListPaged(ctx context.Context, offset int64, limit int, before time.Time, status payments.CouponStatus) (_ payments.CouponsPage, err error) {
	defer mon.Task()(&ctx)(&err)

	var page payments.CouponsPage

	dbxCoupons, err := coupons.db.Limited_Coupon_By_CreatedAt_LessOrEqual_And_Status_OrderBy_Desc_CreatedAt(
		ctx,
		dbx.Coupon_CreatedAt(before.UTC()),
		dbx.Coupon_Status(coinpayments.StatusPending.Int()),
		limit+1,
		offset,
	)
	if err != nil {
		return payments.CouponsPage{}, err
	}

	if len(dbxCoupons) == limit+1 {
		page.Next = true
		page.NextOffset = offset + int64(limit) + 1

		dbxCoupons = dbxCoupons[:len(dbxCoupons)-1]
	}

	page.Coupons, err = couponsFromDbxSlice(dbxCoupons)
	if err != nil {
		return payments.CouponsPage{}, nil
	}

	return page, nil
}

// ListByProjectID returns all active coupons for specified project.
func (coupons *coupons) ListByProjectID(ctx context.Context, projectID uuid.UUID) (_ []payments.Coupon, err error) {
	defer mon.Task()(&ctx, projectID)(&err)

	dbxCoupons, err := coupons.db.All_Coupon_By_ProjectId_And_Status_Equal_Number_OrderBy_Desc_CreatedAt(
		ctx,
		dbx.Coupon_ProjectId(projectID[:]),
	)
	if err != nil {
		return nil, err
	}

	return couponsFromDbxSlice(dbxCoupons)
}

// fromDBXCoupon converts *dbx.Coupon to *payments.Coupon.
func fromDBXCoupon(dbxCoupon *dbx.Coupon) (coupon payments.Coupon, err error) {
	coupon.UserID, err = dbutil.BytesToUUID(dbxCoupon.UserId)
	if err != nil {
		return payments.Coupon{}, err
	}

	coupon.ProjectID, err = dbutil.BytesToUUID(dbxCoupon.ProjectId)
	if err != nil {
		return payments.Coupon{}, err
	}

	coupon.ID, err = dbutil.BytesToUUID(dbxCoupon.Id)
	if err != nil {
		return payments.Coupon{}, err
	}

	coupon.Duration = int(dbxCoupon.Duration)
	coupon.Description = dbxCoupon.Description
	coupon.Amount = dbxCoupon.Amount
	coupon.Created = dbxCoupon.CreatedAt
	coupon.Status = payments.CouponStatus(dbxCoupon.Status)

	return coupon, nil
}

// AddUsage creates new coupon usage record in database.
func (coupons *coupons) AddUsage(ctx context.Context, usage stripecoinpayments.CouponUsage) (err error) {
	defer mon.Task()(&ctx, usage)(&err)

	_, err = coupons.db.Create_CouponUsage(
		ctx,
		dbx.CouponUsage_CouponId(usage.CouponID[:]),
		dbx.CouponUsage_Amount(usage.Amount),
		dbx.CouponUsage_Status(int(usage.Status)),
		dbx.CouponUsage_Period(usage.Period),
	)

	return err
}

// TotalUsage gets sum of all usage records for specified coupon.
func (coupons *coupons) TotalUsage(ctx context.Context, couponID uuid.UUID) (_ int64, err error) {
	defer mon.Task()(&ctx, couponID)(&err)

	query := coupons.db.Rebind(
		`SELECT COALESCE(SUM(amount), 0)
			  FROM coupon_usages
			  WHERE coupon_id = ?;`,
	)

	amountRow := coupons.db.QueryRowContext(ctx, query, couponID[:])

	var amount int64
	err = amountRow.Scan(&amount)

	return amount, err
}

// TotalUsage gets sum of all usage records for specified coupon.
func (coupons *coupons) TotalUsageForPeriod(ctx context.Context, couponID uuid.UUID, period time.Time) (_ int64, err error) {
	defer mon.Task()(&ctx, couponID)(&err)

	query := coupons.db.Rebind(
		`SELECT COALESCE(SUM(amount), 0)
			  FROM coupon_usages
			  WHERE coupon_id = ?;`,
	)

	amountRow := coupons.db.QueryRowContext(ctx, query, couponID[:])

	var amount int64
	err = amountRow.Scan(&amount)

	return amount, err
}

// GetLatest return period_end of latest coupon charge.
func (coupons *coupons) GetLatest(ctx context.Context, couponID uuid.UUID) (_ time.Time, err error) {
	defer mon.Task()(&ctx, couponID)(&err)

	query := coupons.db.Rebind(
		`SELECT period 
			  FROM coupon_usages 
			  WHERE coupon_id = ? 
			  ORDER BY period DESC
			  LIMIT 1;`,
	)

	amountRow := coupons.db.QueryRowContext(ctx, query, couponID[:])

	var created time.Time
	err = amountRow.Scan(&created)
	if err == sql.ErrNoRows {
		return created, stripecoinpayments.ErrNoCouponUsages.Wrap(err)
	}

	return created, err
}

// ListUnapplied returns coupon usage page with unapplied coupon usages.
func (coupons *coupons) ListUnapplied(ctx context.Context, offset int64, limit int, before time.Time) (_ stripecoinpayments.CouponUsagePage, err error) {
	defer mon.Task()(&ctx, offset, limit, before)(&err)

	var page stripecoinpayments.CouponUsagePage

	dbxRecords, err := coupons.db.Limited_CouponUsage_By_Period_LessOrEqual_And_Status_Equal_Number_OrderBy_Desc_Period(
		ctx,
		dbx.CouponUsage_Period(before),
		limit+1,
		offset,
	)
	if err != nil {
		return stripecoinpayments.CouponUsagePage{}, err
	}

	if len(dbxRecords) == limit+1 {
		page.Next = true
		page.NextOffset = offset + int64(limit) + 1

		dbxRecords = dbxRecords[:len(dbxRecords)-1]
	}

	for _, dbxRecord := range dbxRecords {
		record, err := couponUsageFromDbxSlice(dbxRecord)
		if err != nil {
			return stripecoinpayments.CouponUsagePage{}, err
		}

		page.Usages = append(page.Usages, record)
	}

	return page, nil
}

// ApplyUsage applies coupon usage and updates its status.
func (coupons *coupons) ApplyUsage(ctx context.Context, couponID uuid.UUID, period time.Time) (err error) {
	defer mon.Task()(&ctx, couponID, period)(&err)

	_, err = coupons.db.Update_CouponUsage_By_CouponId_And_Period(
		ctx,
		dbx.CouponUsage_CouponId(couponID[:]),
		dbx.CouponUsage_Period(period),
		dbx.CouponUsage_Update_Fields{
			Status: dbx.CouponUsage_Status(int(stripecoinpayments.CouponUsageStatusApplied)),
		},
	)

	return err
}

// couponsFromDbxSlice is used for creating []payments.Coupon entities from autogenerated []dbx.Coupon struct.
func couponsFromDbxSlice(couponsDbx []*dbx.Coupon) (_ []payments.Coupon, err error) {
	var coupons = make([]payments.Coupon, 0)
	var errors []error

	// Generating []dbo from []dbx and collecting all errors
	for _, couponDbx := range couponsDbx {
		coupon, err := fromDBXCoupon(couponDbx)
		if err != nil {
			errors = append(errors, err)
			continue
		}

		coupons = append(coupons, coupon)
	}

	return coupons, errs.Combine(errors...)
}

// couponUsageFromDbxSlice is used for creating stripecoinpayments.CouponUsage entity from autogenerated dbx.CouponUsage struct.
func couponUsageFromDbxSlice(couponUsageDbx *dbx.CouponUsage) (usage stripecoinpayments.CouponUsage, err error) {
	usage.Status = stripecoinpayments.CouponUsageStatus(couponUsageDbx.Status)
	usage.Period = couponUsageDbx.Period
	usage.Amount = couponUsageDbx.Amount

	usage.CouponID, err = dbutil.BytesToUUID(couponUsageDbx.CouponId)
	if err != nil {
		return stripecoinpayments.CouponUsage{}, err
	}

	return usage, err
}

// PopulatePromotionalCoupons is used to populate promotional coupons through all active users who already have a project
// and do not have a promotional coupon yet. And updates project limits to selected size.
func (coupons *coupons) PopulatePromotionalCoupons(ctx context.Context, users []uuid.UUID, duration int, amount int64, projectLimit memory.Size) (err error) {
	defer mon.Task()(&ctx, users, duration, amount, projectLimit)(&err)

	// converting to []interface{} instead of [][]byte
	// to pass it to QueryContext as usersIDs...
	var usersIDs []interface{}
	for i := range users {
		usersIDs = append(usersIDs, users[i][:])
	}

	query := coupons.db.Rebind(fmt.Sprintf(
		`SELECT users_with_projects.id, users_with_projects.project_id FROM (
				 SELECT selected_users.id, first_proj.id as project_id
				 FROM (select id, status from users where id in `+generateArgumentsDBX(len(users))+`) as selected_users INNER JOIN
  				     (select distinct on (owner_id) owner_id, id from projects order by owner_id, created_at asc) as first_proj
  				 ON (selected_users.id = first_proj.owner_id)
				 WHERE selected_users.status = %d) AS users_with_projects
			  WHERE users_with_projects.id NOT IN (
				 SELECT user_id FROM coupons WHERE type = %d
			  );`, console.Active, payments.CouponTypePromotional),
	)

	usersIDsRows, err := coupons.db.QueryContext(ctx, query, usersIDs...)
	if err != nil {
		return err
	}
	defer func() { err = errs.Combine(err, usersIDsRows.Close()) }()

	type UserAndProjectIDs struct {
		userID    uuid.UUID
		projectID uuid.UUID
	}

	var ids []UserAndProjectIDs

	for usersIDsRows.Next() {
		var userID uuid.UUID
		var userIDBytes []byte
		var projectID uuid.UUID
		var projectIDBytes []byte

		err = usersIDsRows.Scan(&userIDBytes, &projectIDBytes)
		if err != nil {
			return err
		}

		userID, err = dbutil.BytesToUUID(userIDBytes)
		if err != nil {
			return err
		}

		projectID, err = dbutil.BytesToUUID(projectIDBytes)
		if err != nil {
			return err
		}

		ids = append(ids, UserAndProjectIDs{userID: userID, projectID: projectID})
	}
	if err = usersIDsRows.Err(); err != nil {
		return err
	}

	return coupons.db.WithTx(ctx, func(ctx context.Context, tx *dbx.Tx) error {
		for _, userAndProjectID := range ids {
			err = coupons.Insert(ctx, payments.Coupon{
				UserID:      userAndProjectID.userID,
				ProjectID:   userAndProjectID.projectID,
				Amount:      amount,
				Duration:    duration,
				Description: fmt.Sprintf("Promotional credits (limited time - %d billing periods)", duration),
				Type:        payments.CouponTypePromotional,
				Status:      payments.CouponActive,
			})
			if err != nil {
				return err
			}

			_, err = coupons.db.Update_Project_By_Id(ctx,
				dbx.Project_Id(userAndProjectID.projectID[:]),
				dbx.Project_Update_Fields{
					UsageLimit: dbx.Project_UsageLimit(projectLimit.Int64()),
				},
			)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// generates variadic number of '?' symbols in brackets depends on input parameter.
func generateArgumentsDBX(length int) string {
	start := "("

	if length >= 1 {
		start += "?"
	}

	middle := ""
	for i := 1; i < length; i++ {
		middle += ", ?"
	}

	end := ")"

	return start + middle + end
}
