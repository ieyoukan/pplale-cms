// Package cards implements the PPLALE-web card data contract: the CardInfo
// schema, dataset file layout, ID allocation and validation rules that the
// pplale-cms must satisfy before opening a pull request.
package cards

import "fmt"

type CardType string

const (
	TypeYojo     CardType = "yojo"
	TypeSweet    CardType = "sweet"
	TypePlayable CardType = "playable"
)

type FruitType string

const (
	FruitAll        FruitType = "all"
	FruitStrawberry FruitType = "strawberry"
	FruitGrape      FruitType = "grape"
	FruitMelon      FruitType = "melon"
	FruitOrange     FruitType = "orange"
)

type CardRole string

const (
	RoleNone             CardRole = ""
	RoleAssistantManager CardRole = "assistant_manager"
	RoleManager          CardRole = "manager"
)

type SweetType string

const (
	SweetNone       SweetType = ""
	SweetAnimalSoda SweetType = "animal_soda"
	SweetCafe       SweetType = "cafe"
	SweetFloat      SweetType = "float"
	SweetDoughnut   SweetType = "doughnut"
	SweetCake       SweetType = "cake"
	SweetBackMenu   SweetType = "back_menu"
	SweetChai       SweetType = "chai"
	SweetIceCream   SweetType = "ice_cream"
	SweetPplaleSoda SweetType = "pplale_soda"
	SweetPplaleYaki SweetType = "pplale_yaki"
	SweetCurrency   SweetType = "currency"
)

type CardVersion string

const (
	VersionNormal CardVersion = "normal"
	VersionBeta   CardVersion = "beta"
)

// The ordered enum slices below are the single definition of each value set:
// validation, the options the API hands the form, and the TypeScript types the
// frontend compiles against all derive from them.
var (
	allCardTypes = []CardType{TypeYojo, TypeSweet, TypePlayable}
	allFruits    = []FruitType{FruitAll, FruitStrawberry, FruitGrape, FruitMelon, FruitOrange}
	allRoles     = []CardRole{RoleNone, RoleAssistantManager, RoleManager}
	allSweetType = []SweetType{SweetNone, SweetAnimalSoda, SweetCafe, SweetFloat, SweetDoughnut, SweetCake, SweetBackMenu, SweetChai, SweetIceCream, SweetPplaleSoda, SweetPplaleYaki, SweetCurrency}
	allVersions  = []CardVersion{VersionNormal, VersionBeta}

	cardTypes   = enumSet(allCardTypes)
	cardRoles   = enumSet(allRoles)
	cardVersion = enumSet(allVersions)
)

// AllCardTypes, AllFruits, AllRoles, AllSweetTypes and AllVersions expose the
// accepted values in the order the UI should offer them.
func AllCardTypes() []CardType   { return append([]CardType(nil), allCardTypes...) }
func AllFruits() []FruitType     { return append([]FruitType(nil), allFruits...) }
func AllRoles() []CardRole       { return append([]CardRole(nil), allRoles...) }
func AllSweetTypes() []SweetType { return append([]SweetType(nil), allSweetType...) }
func AllVersions() []CardVersion { return append([]CardVersion(nil), allVersions...) }

func enumSet[T ~string](values []T) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[string(v)] = struct{}{}
	}
	return set
}

// Card mirrors CardInfo in PPLALE-web (src/types/card.ts). Optional fields are
// pointers so that a card read from a dataset file round-trips byte-for-byte:
// the existing files carry `"role": ""` on yojo cards, and omitting it would
// produce a noisy diff in the generated pull request.
type Card struct {
	ID          string
	Name        string
	Type        CardType
	Version     *CardVersion
	Fruit       FruitType
	Description string
	ImageURL    string
	Cost        int
	HP          int
	Attack      int
	Effect      *string
	Role        *CardRole
	SweetType   *SweetType
}

// Kind identifies one dataset file. It is finer grained than CardType because
// token yojo cards live in their own file while still carrying type "yojo".
type Kind string

const (
	KindYojo      Kind = "yojo"
	KindSweet     Kind = "sweet"
	KindPlayable  Kind = "playable"
	KindTokenYojo Kind = "tokenYojo"
)

// Dataset describes where a Kind is stored inside the PPLALE-web repository
// and how its cards are identified.
type Dataset struct {
	Kind Kind
	// JSONPath is the repository-relative path of the dataset file.
	JSONPath string
	// RootKey is the single top-level object key inside that file.
	RootKey string
	// IDPrefix is the prefix every ID in this dataset carries.
	IDPrefix string
	// CardType is the `type` field value every card in this dataset carries.
	CardType CardType
	// ImageDir is the directory under public/images and public/og-cards that
	// holds this dataset's images. Token yojo shares the yojo directory.
	ImageDir string
}

var datasets = map[Kind]Dataset{
	KindYojo:      {Kind: KindYojo, JSONPath: "src/data/yojo.json", RootKey: "yojo", IDPrefix: "y_", CardType: TypeYojo, ImageDir: "yojo"},
	KindSweet:     {Kind: KindSweet, JSONPath: "src/data/sweet.json", RootKey: "sweet", IDPrefix: "s_", CardType: TypeSweet, ImageDir: "sweet"},
	KindPlayable:  {Kind: KindPlayable, JSONPath: "src/data/playable.json", RootKey: "playable", IDPrefix: "p_", CardType: TypePlayable, ImageDir: "playable"},
	KindTokenYojo: {Kind: KindTokenYojo, JSONPath: "src/data/tokenYojo.json", RootKey: "tokenYojo", IDPrefix: "yt_", CardType: TypeYojo, ImageDir: "yojo"},
}

// AllKinds lists every dataset kind in a stable order.
func AllKinds() []Kind {
	return []Kind{KindYojo, KindSweet, KindPlayable, KindTokenYojo}
}

// DatasetFor returns the dataset descriptor for a kind.
func DatasetFor(kind Kind) (Dataset, error) {
	ds, ok := datasets[kind]
	if !ok {
		return Dataset{}, fmt.Errorf("cards: unknown dataset kind %q", kind)
	}
	return ds, nil
}

// ImagePath returns the repository-relative path of the WebP card image for a
// file name such as "sample.webp".
func (d Dataset) ImagePath(fileName string) string {
	return "public/images/" + d.ImageDir + "/" + fileName
}

// OGImagePath returns the repository-relative path of the 240px PNG mirror the
// OGP renderer reads. The CI does not verify it, so it must never be skipped.
func (d Dataset) OGImagePath(fileName string) string {
	return "public/og-cards/" + d.ImageDir + "/" + fileName
}

// ImageURL returns the `imageUrl` field value for a WebP file name.
func (d Dataset) ImageURL(fileName string) string {
	return "/images/" + d.ImageDir + "/" + fileName
}

// RepoImagePath converts a card's `imageUrl` field (e.g. "/images/yojo/x.webp",
// the site-relative form PPLALE-web's Next.js app serves) into the path of
// that file inside the repository (e.g. "public/images/yojo/x.webp"). It does
// not validate the input; callers that accept imageUrl from outside this
// package should run it through Validate first.
func RepoImagePath(imageURL string) string {
	return "public" + imageURL
}

func ptr[T any](v T) *T { return &v }
