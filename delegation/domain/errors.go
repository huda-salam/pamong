package domain

import "github.com/huda-salam/pamong/core"

// Error domain delegasi. Memakai core.ErrValidation agar konsisten dgn lapis lain (gateway
// memetakannya ke 400).
var (
	ErrFromUserKosong        = core.ErrValidation("from_user_id", "tidak boleh kosong")
	ErrToUserKosong          = core.ErrValidation("to_user_id", "tidak boleh kosong")
	ErrDelegasiKeDiriSendiri = core.ErrValidation("to_user_id", "tidak boleh sama dengan from_user_id")
	ErrAssignedByKosong      = core.ErrValidation("assigned_by", "tidak boleh kosong")
	ErrPermissionsKosong     = core.ErrValidation("permissions", "minimal satu permission didelegasikan")
	ErrValidUntilWajib       = core.ErrValidation("valid_until", "delegasi wajib berbatas waktu")
	ErrPeriodeTerbalik       = core.ErrValidation("valid_until", "harus setelah valid_from")
	ErrPermNonDelegable      = core.ErrValidation("permissions", "memuat permission yang tak boleh didelegasikan")
)
