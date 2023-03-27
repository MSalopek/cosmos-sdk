package decode

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
)

const bit11NonCritical = 1 << 10

var anyDesc = (&anypb.Any{}).ProtoReflect().Descriptor()
var anyFullName = anyDesc.FullName()

func RejectUnknownFieldsStrict(bz []byte, msg protoreflect.MessageDescriptor, resolver protodesc.Resolver) error {
	var _, err = RejectUnknownFields(bz, msg, false, resolver)
	return err
}

// RejectUnknownFields rejects any bytes bz with an error that has unknown fields for the provided proto.Message type with an
// option to allow non-critical fields (specified as those fields with bit 11) to pass through. In either case, the
// hasUnknownNonCriticals will be set to true if non-critical fields were encountered during traversal. This flag can be
// used to treat a message with non-critical field different in different security contexts (such as transaction signing).
// This function traverses inside of messages nested via google.protobuf.Any. It does not do any deserialization of the proto.Message.
// An AnyResolver must be provided for traversing inside google.protobuf.Any's.
func RejectUnknownFields(bz []byte, desc protoreflect.MessageDescriptor, allowUnknownNonCriticals bool, resolver protodesc.Resolver) (hasUnknownNonCriticals bool, err error) {
	if len(bz) == 0 {
		return hasUnknownNonCriticals, nil
	}

	//fieldDescProtoFromTagNum, _, err := getDescriptorInfo(desc, msg)
	fields := desc.Fields()

	for len(bz) > 0 {
		tagNum, wireType, m := protowire.ConsumeTag(bz)
		if m < 0 {
			return hasUnknownNonCriticals, errors.New("invalid length")
		}
		fmt.Printf("tagNum: %d, wireType: %d, m: %d\n", tagNum, wireType, m)

		//fieldDescProto, ok := fieldDescProtoFromTagNum[int32(tagNum)]

		fieldDesc := fields.ByNumber(tagNum)
		if fieldDesc == nil {
			isCriticalField := tagNum&bit11NonCritical == 0

			if !isCriticalField {
				hasUnknownNonCriticals = true
			}

			if isCriticalField || !allowUnknownNonCriticals {
				// The tag is critical, so report it.
				return hasUnknownNonCriticals, ErrUnknownField.Wrapf(
					"%s: {TagNum: %d, WireType:%q}",
					desc.FullName(), tagNum, WireTypeToString(wireType))
			}
		}

		// Skip over the bytes that store fieldNumber and wireType bytes.
		bz = bz[m:]
		n := protowire.ConsumeFieldValue(tagNum, wireType, bz)
		if n < 0 {
			err = fmt.Errorf("could not consume field value for tagNum: %d, wireType: %q; %w",
				tagNum, WireTypeToString(wireType), protowire.ParseError(n))
			return hasUnknownNonCriticals, err
		}
		fieldBytes := bz[:n]
		bz = bz[n:]

		// An unknown but non-critical field
		if fieldDesc == nil {
			continue
		}

		fieldName := fieldDesc.FullName()
		fmt.Printf("fieldName: %s, tagNum: %d, wireType: %s, n: %d\n",
			fieldName, tagNum, WireTypeToString(wireType), n)

		if !isWireTypeAssignable(wireType, fieldDesc.Kind()) {
			return hasUnknownNonCriticals, fmt.Errorf(
				"invalid wire type %s for field %s", WireTypeToString(wireType), fieldDesc.FullName())
		}

		fieldMessage := fieldDesc.Message()
		// not message or group kind
		if fieldMessage == nil {
			continue
		}

		// consume length prefix of nested message
		_, o := protowire.ConsumeVarint(fieldBytes)
		fieldBytes = fieldBytes[o:]

		var err error

		if fieldMessage.FullName() == anyFullName {
			// Firstly typecheck types.Any to ensure nothing snuck in.
			hasUnknownNonCriticalsChild, err := RejectUnknownFields(fieldBytes, anyDesc, allowUnknownNonCriticals, resolver)
			hasUnknownNonCriticals = hasUnknownNonCriticals || hasUnknownNonCriticalsChild
			if err != nil {
				return hasUnknownNonCriticals, err
			}
			var a anypb.Any
			if err = proto.Unmarshal(fieldBytes, &a); err != nil {
				return hasUnknownNonCriticals, err
			}

			msgName := MessageNameFromTypeURL(a.TypeUrl)
			msgDesc, err := resolver.FindDescriptorByName(msgName)
			if err != nil {
				return hasUnknownNonCriticals, err
			}

			fieldMessage = msgDesc.(protoreflect.MessageDescriptor)
			fieldBytes = a.Value
		}

		hasUnknownNonCriticalsChild, err := RejectUnknownFields(fieldBytes, fieldMessage, allowUnknownNonCriticals, resolver)
		hasUnknownNonCriticals = hasUnknownNonCriticals || hasUnknownNonCriticalsChild
		if err != nil {
			return hasUnknownNonCriticals, err
		}
	}

	return hasUnknownNonCriticals, nil
}

// errUnknownField represents an error indicating that we encountered
// a field that isn't available in the target proto.Message.
type errUnknownField struct {
	Desc     protoreflect.MessageDescriptor
	TagNum   protowire.Number
	WireType protowire.Type
}

// String implements fmt.Stringer.
func (twt *errUnknownField) String() string {
	return fmt.Sprintf("errUnknownField %q: {TagNum: %d, WireType:%q}",
		twt.Desc.FullName(), twt.TagNum, WireTypeToString(twt.WireType))
}

// Error implements the error interface.
func (twt *errUnknownField) Error() string {
	return twt.String()
}

var _ error = (*errUnknownField)(nil)

func WireTypeToString(wt protowire.Type) string {
	switch wt {
	case 0:
		return "varint"
	case 1:
		return "fixed64"
	case 2:
		return "bytes"
	case 3:
		return "start_group"
	case 4:
		return "end_group"
	case 5:
		return "fixed32"
	default:
		return fmt.Sprintf("unknown type: %d", wt)
	}
}

func isWireTypeAssignable(wt protowire.Type, kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.BoolKind, protoreflect.EnumKind:
		return wt == protowire.VarintType

	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return wt == protowire.Fixed64Type

	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		return wt == protowire.BytesType

	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind, protoreflect.FloatKind:
		return wt == protowire.Fixed32Type
	}

	return false
}
