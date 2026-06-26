package domain

import "github.com/huda-salam/pamong/core"

// Error domain role tenant. Memakai core.ErrValidation agar konsisten dgn lapis lain
// (gateway memetakannya ke 400). Pesan sengaja selaras dgn padanan role sentral.
var (
	ErrTenantRoleNameInvalid = core.ErrValidation("name", "harus snake_case, mulai huruf, 3-100 char (a-z0-9_)")
	ErrTenantRoleLabelKosong = core.ErrValidation("label", "tidak boleh kosong")
	ErrUserIDKosong          = core.ErrValidation("user_id", "tidak boleh kosong")
	ErrRoleIDKosong          = core.ErrValidation("role_id", "tidak boleh kosong")
	ErrAssignedByKosong      = core.ErrValidation("assigned_by", "tidak boleh kosong")
)
