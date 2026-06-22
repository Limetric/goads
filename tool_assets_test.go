package main

import "testing"

func TestUploadImageAsset_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := AssetImageArgs{CustomerID: "123-456-7890", AssetName: "Logo", ImageDataBase64: "iVBORw0KGgo"}
	prev, err := runUploadImageAsset(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.Applied || prev.Token == "" || cap.calls != 0 {
		t.Fatalf("bad preview: %+v calls=%d", prev, cap.calls)
	}

	args.Confirm = prev.Token
	done, err := runUploadImageAsset(t.Context(), c, args)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !done.Applied || cap.calls != 1 {
		t.Fatalf("apply failed: %+v calls=%d", done, cap.calls)
	}
	create := opCreate(t, cap.firstOp(t), "assetOperation")
	if create["name"] != "Logo" || create["type"] != "IMAGE" {
		t.Errorf("unexpected asset op: %v", create)
	}
	img, _ := create["imageAsset"].(map[string]any)
	if img == nil || img["data"] != "iVBORw0KGgo" {
		t.Errorf("image data missing: %v", create)
	}
}

func TestUploadTextAsset_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := AssetTextArgs{CustomerID: "1", AssetName: "Headline", TextContent: "Buy Now"}
	prev, err := runUploadTextAsset(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runUploadTextAsset(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "assetOperation")
	txt, _ := create["textAsset"].(map[string]any)
	if txt == nil || txt["text"] != "Buy Now" {
		t.Errorf("text asset missing: %v", create)
	}
}

func TestUploadAsset_Validation(t *testing.T) {
	if _, err := runUploadImageAsset(t.Context(), nil, AssetImageArgs{CustomerID: "1", ImageDataBase64: "x"}); err == nil {
		t.Error("empty asset name should error")
	}
	if _, err := runUploadImageAsset(t.Context(), nil, AssetImageArgs{CustomerID: "1", AssetName: "L"}); err == nil {
		t.Error("empty image data should error")
	}
	if _, err := runUploadTextAsset(t.Context(), nil, AssetTextArgs{CustomerID: "1", AssetName: "N"}); err == nil {
		t.Error("empty text content should error")
	}
}

func TestUploadAsset_Blocked(t *testing.T) {
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "upload_image_asset")
	if _, err := runUploadImageAsset(t.Context(), nil, AssetImageArgs{CustomerID: "1", AssetName: "L", ImageDataBase64: "x"}); err == nil {
		t.Fatal("expected blocked-operation error")
	}
}
