package signaling

import (
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var ErrPeerNotFound = errors.New("peer not found")
var ErrGroupNotFound = errors.New("group not found")
var ErrGroupAlreadyExists = errors.New("group already exists")
var ErrMemberLacksGroup = errors.New("member lacks group")
var ErrInvalidMessageType = errors.New("invalid message type")
var ErrNonNilGroupRequired = errors.New("non-nil group required")

const (
	RoomDefaultGroupID = GroupID("default")
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 . RoomMember

type RoomMember interface {
	GetGroup() *Group
	SetGroup(*Group)

	GroupMember
}

type Room struct {
	groups     map[GroupID]*Group
	groupsLock *sync.RWMutex
}

func NewRoom() *Room {
	return &Room{
		groups: map[GroupID]*Group{},
		// If we avoid creating groups, except for during startup, this mutex won't be needed
		groupsLock: &sync.RWMutex{},
	}
}

// Clients can disconenct without a leave event, iterate groups and tell them to
// remove stale members
func (r *Room) StartReaper(interval time.Duration) {
	go func() {
		for {
			logrus.Debugf("running reaper")

			r.groupsLock.RLock()

			for _, group := range r.groups {
				group.PruneStaleMembers()
			}

			r.groupsLock.RUnlock()

			time.Sleep(interval)
		}
	}()
}

func (r *Room) Dispatch(member RoomMember, message Message) error {
	logrus.Debugf("Message type: %s", message.Type)

	switch message.Type {
	case MessageTypeJoin:
		group := member.GetGroup()
		if group != nil {
			group.RemoveMember(member)
			member.SetGroup(nil)
		}

		groupID := GetGroupIDFromMessage(message, RoomDefaultGroupID)
		group = r.GetGroup(groupID)
		if group == nil {
			return ErrGroupNotFound
		}

		member.SetGroup(group)
		group.AddMember(member)

		err := group.Broadcast(message)
		if err != nil {
			logrus.Error(err)
		}
	case MessageTypeLeave:
		group := member.GetGroup()
		if group == nil {
			return ErrMemberLacksGroup
		}

		group.RemoveMember(member)

		err := group.Broadcast(message)
		if err != nil {
			logrus.Error(err)
		}
	case MessageTypeOffer, MessageTypeAnswer, MessageTypeICECandidate:
		group := member.GetGroup()
		if group == nil {
			return ErrMemberLacksGroup
		}

		err := group.MessageMember(message)
		if err != nil {
			logrus.Error(err)
		}
	default:
		logrus.Warnf(`unknown message type %s`, message.Type)
		return ErrInvalidMessageType
	}

	return nil
}

func (r *Room) AddGroup(group *Group) error {
	if group == nil {
		return ErrNonNilGroupRequired
	}

	r.groupsLock.Lock()
	defer r.groupsLock.Unlock()

	_, ok := r.groups[group.ID()]
	if ok {
		return ErrGroupAlreadyExists
	}

	r.groups[group.ID()] = group

	return nil
}

func (r *Room) GetGroup(groupID GroupID) *Group {
	r.groupsLock.RLock()
	defer r.groupsLock.RUnlock()

	group, ok := r.groups[groupID]
	if !ok {
		return nil
	}

	return group
}
